#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Configuration
CLUSTER_NAME="${KIND_CLUSTER_NAME:-xds-e2e}"
NAMESPACE="xds-system"
TIMEOUT="${E2E_TIMEOUT:-300}"  # 5 minutes default

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
}

# Wait for deployment to be ready
wait_for_deployment() {
    local name=$1
    local timeout=${2:-120}
    log_info "Waiting for deployment $name to be ready..."
    kubectl rollout status deployment/"$name" -n "$NAMESPACE" --timeout="${timeout}s"
}

# Wait for pods with label to be ready
wait_for_pods() {
    local label=$1
    local timeout=${2:-120}
    log_info "Waiting for pods with label $label..."
    kubectl wait --for=condition=ready pod -l "$label" -n "$NAMESPACE" --timeout="${timeout}s"
}

# Test HTTP request and verify response
test_http_request() {
    local url=$1
    local expected_code=$2
    local description=$3
    local extra_args=${4:-}
    
    local response_code
    response_code=$(curl -s -o /dev/null -w "%{http_code}" $extra_args "$url" --max-time 10 || echo "000")
    
    if [[ "$response_code" == "$expected_code" ]]; then
        log_info "✓ $description (status: $response_code)"
        return 0
    else
        log_warn "✗ $description - expected $expected_code, got $response_code"
        return 1
    fi
}

# Test HTTP request and check response header
test_http_header() {
    local url=$1
    local header_name=$2
    local expected_value=$3
    local description=$4
    local extra_args=${5:-}
    
    local header_value
    header_value=$(curl -s -D - -o /dev/null $extra_args "$url" --max-time 10 2>/dev/null | grep -i "^${header_name}:" | cut -d' ' -f2- | tr -d '\r\n' || echo "")
    
    if [[ "$header_value" == *"$expected_value"* ]]; then
        log_info "✓ $description (header $header_name: $header_value)"
        return 0
    else
        log_warn "✗ $description - header $header_name expected '$expected_value', got '$header_value'"
        return 1
    fi
}

# Main test function
run_tests() {
    log_info "=== Starting E2E Tests ==="
    
    # Step 1: Create KIND cluster
    log_info "Step 1: Creating KIND cluster..."
    kind create cluster --name "$CLUSTER_NAME" --wait 60s
    
    # Step 2: Build and load xDS controller image
    log_info "Step 2: Building xDS controller image..."
    docker build -t xds-controller:e2e .
    kind load docker-image xds-controller:e2e --name "$CLUSTER_NAME"
    
    # Step 3: Apply CRDs
    log_info "Step 3: Applying CRDs..."
    kubectl apply -f config/crd/bases/
    
    # Step 4: Create namespace and deploy xDS controller + backend
    log_info "Step 4: Deploying xDS controller and test backend..."
    kubectl apply -f test/e2e/manifests/namespace.yaml
    kubectl apply -f test/e2e/manifests/xds-controller.yaml
    kubectl apply -f test/e2e/manifests/test-backend.yaml
    
    # Step 5: Wait for xDS controller to be ready
    log_info "Step 5: Waiting for xDS controller to be ready..."
    wait_for_deployment "xds-controller" 120
    wait_for_deployment "test-backend" 60
    
    # Step 6: Apply xDS resources BEFORE Envoy connects
    log_info "Step 6: Applying xDS resources (before Envoy starts)..."
    kubectl apply -f test/e2e/manifests/xds-resources.yaml
    
    # Step 6b: Apply complex production-like xDS resources
    log_info "Step 6b: Applying complex production-like xDS resources..."
    kubectl apply -f test/e2e/manifests/xds-complex-resources.yaml
    
    # Step 7: Wait for xDS controller to reconcile resources
    log_info "Step 7: Waiting for xDS controller to reconcile resources..."
    sleep 10
    
    # Verify xDS controller has processed resources
    log_info "Checking xDS controller has processed resources..."
    kubectl logs -l app=xds-controller -n "$NAMESPACE" --tail=20 | grep -E "Updated|Upgrading" || true
    
    # Step 8: NOW deploy Envoy (after xDS snapshot is ready)
    log_info "Step 8: Deploying Envoy (xDS snapshot should be ready)..."
    kubectl apply -f test/e2e/manifests/envoy.yaml
    wait_for_deployment "envoy" 120
    
    # Step 9: Wait for Envoy to receive configuration
    log_info "Step 9: Waiting for Envoy to receive xDS configuration..."
    sleep 10
    
    # Step 10: Check xDS controller logs for any errors
    log_info "Step 10: Checking xDS controller logs..."
    kubectl logs -l app=xds-controller -n "$NAMESPACE" --tail=50 || true
    
    # Get Envoy connection details
    local envoy_admin_port
    envoy_admin_port=$(kubectl get svc envoy -n "$NAMESPACE" -o jsonpath='{.spec.ports[?(@.name=="admin")].nodePort}')
    
    local envoy_http_port
    envoy_http_port=$(kubectl get svc envoy -n "$NAMESPACE" -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
    
    local envoy_http_complex_port
    envoy_http_complex_port=$(kubectl get svc envoy -n "$NAMESPACE" -o jsonpath='{.spec.ports[?(@.name=="http-complex")].nodePort}')
    
    local envoy_https_port
    envoy_https_port=$(kubectl get svc envoy -n "$NAMESPACE" -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')
    
    local node_ip
    node_ip=$(docker inspect "${CLUSTER_NAME}-control-plane" --format '{{.NetworkSettings.Networks.kind.IPAddress}}')
    
    log_info "Envoy admin URL: http://${node_ip}:${envoy_admin_port}"
    log_info "Envoy HTTP (basic) URL: http://${node_ip}:${envoy_http_port}"
    log_info "Envoy HTTP (complex) URL: http://${node_ip}:${envoy_http_complex_port}"
    log_info "Envoy HTTPS URL: https://${node_ip}:${envoy_https_port}"
    
    # Step 11: Verify clusters are configured
    log_info "Step 11: Checking Envoy clusters..."
    local clusters_output
    local max_retries=12
    local retry_interval=5
    local retry_count=0
    local cluster_found=false
    
    while [[ $retry_count -lt $max_retries ]]; do
        clusters_output=$(curl -s "http://${node_ip}:${envoy_admin_port}/clusters" || echo "FAILED")
        
        if echo "$clusters_output" | grep -q "complex-cluster"; then
            cluster_found=true
            log_info "✓ Cluster 'complex-cluster' found (attempt $((retry_count + 1)))"
            break
        fi
        
        retry_count=$((retry_count + 1))
        if [[ $retry_count -lt $max_retries ]]; then
            log_warn "Cluster not found yet, retrying in ${retry_interval}s... (attempt $retry_count/$max_retries)"
            sleep $retry_interval
        fi
    done
    
    if [[ "$cluster_found" != "true" ]]; then
        log_error "✗ Cluster 'complex-cluster' NOT found after $max_retries attempts"
        echo "Clusters output: $clusters_output"
        kubectl logs -l app=xds-controller -n "$NAMESPACE" --tail=50 || true
        return 1
    fi
    
    # Check all expected clusters
    log_info "Verifying all clusters..."
    for cluster_name in "complex-cluster" "simple-cluster" "tcp-cluster" "test-backend-cluster"; do
        if echo "$clusters_output" | grep -q "$cluster_name"; then
            log_info "✓ Cluster '$cluster_name' found"
        else
            log_warn "⚠ Cluster '$cluster_name' not found"
        fi
    done
    
    # Step 12: Verify listeners are configured
    log_info "Step 12: Checking Envoy listeners..."
    local listeners_output
    listeners_output=$(curl -s "http://${node_ip}:${envoy_admin_port}/listeners" || echo "FAILED")
    
    for listener_addr in "0.0.0.0:8080" "0.0.0.0:8081" "0.0.0.0:8443" "0.0.0.0:8444" "0.0.0.0:9000"; do
        if echo "$listeners_output" | grep -q "$listener_addr"; then
            log_info "✓ Listener on $listener_addr found"
        else
            log_warn "⚠ Listener on $listener_addr not found"
        fi
    done
    
    # Step 13: Test actual HTTP traffic through routes
    log_info "Step 13: Testing HTTP traffic through various routes..."
    
    local test_failures=0
    
    # Test 1: Basic route via original listener (port 8080)
    log_info "--- Test: Basic route (original listener) ---"
    test_http_request "http://${node_ip}:${envoy_http_port}/" "200" "Basic route on port 8080" || test_failures=$((test_failures + 1))
    
    # Test 2: Complex route via complex listener (port 8081)
    log_info "--- Test: Complex route (complex listener) ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/" "200" "Complex route on port 8081" || test_failures=$((test_failures + 1))
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/" "x-e2e-test" "api-route" "Response header x-e2e-test" || test_failures=$((test_failures + 1))
    
    # Test 3: API route (/api/) via complex listener
    log_info "--- Test: API route ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/api/" "200" "API route (/api/)" || test_failures=$((test_failures + 1))
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/api/" "x-e2e-test" "api-route" "API route response header" || test_failures=$((test_failures + 1))
    
    # Test 4: Health endpoint via complex listener
    log_info "--- Test: Health endpoint ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/health" "200" "Health endpoint (/health)" || test_failures=$((test_failures + 1))
    
    # Test 5: CORS preflight request
    log_info "--- Test: CORS preflight ---"
    local cors_response
    cors_response=$(curl -s -D - -o /dev/null -X OPTIONS \
        -H "Origin: http://test.example.com" \
        -H "Access-Control-Request-Method: POST" \
        -H "Access-Control-Request-Headers: X-Custom-Header" \
        "http://${node_ip}:${envoy_http_complex_port}/api/" --max-time 10 2>/dev/null || echo "")
    
    if echo "$cors_response" | grep -qi "access-control-allow"; then
        log_info "✓ CORS headers present in preflight response"
    else
        log_warn "✗ CORS headers not found in preflight response"
        test_failures=$((test_failures + 1))
    fi
    
    # Test 6: Request with custom headers (verify they pass through)
    log_info "--- Test: Custom headers ---"
    local custom_header_response
    custom_header_response=$(curl -s -D - -o /dev/null \
        -H "X-Custom-Header: test-value" \
        "http://${node_ip}:${envoy_http_complex_port}/api/" --max-time 10 2>/dev/null || echo "")
    
    if echo "$custom_header_response" | grep -q "200"; then
        log_info "✓ Request with custom headers succeeded"
    else
        log_warn "✗ Request with custom headers failed"
        test_failures=$((test_failures + 1))
    fi
    
    # Test 6: HTTPS route (with self-signed cert - use -k to skip validation)
    log_info "--- Test: HTTPS route ---"
    # Use --resolve to provide SNI and IP resolution
    local https_code
    https_code=$(curl -s -k -o /dev/null -w "%{http_code}" \
        --connect-to "secure.e2e.local:${envoy_https_port}:${node_ip}:${envoy_https_port}" \
        "https://secure.e2e.local:${envoy_https_port}/" --max-time 15 2>/dev/null || echo "000")
    
    if [[ "$https_code" == "200" ]]; then
        log_info "✓ HTTPS route with TLS responded successfully (status: $https_code)"
    elif [[ "$https_code" != "000" ]]; then
        # Got some response (even error), TLS is working
        log_info "✓ HTTPS/TLS connection established (status: $https_code - backend may not have path)"
    else
        # Try direct IP with SNI header
        https_code=$(curl -s -k -o /dev/null -w "%{http_code}" \
            -H "Host: secure.e2e.local" \
            "https://${node_ip}:${envoy_https_port}/" --max-time 15 2>/dev/null || echo "000")
        if [[ "$https_code" != "000" ]]; then
            log_info "✓ HTTPS/TLS working (status: $https_code)"
        else
            log_warn "✗ HTTPS route connection failed"
            test_failures=$((test_failures + 1))
        fi
    fi
    
    # Test 7: Redirect path
    log_info "--- Test: Redirect path ---"
    local redirect_response
    redirect_response=$(curl -s -D - -o /dev/null \
        "http://${node_ip}:${envoy_http_complex_port}/old/path" --max-time 10 2>/dev/null || echo "")
    
    if echo "$redirect_response" | grep -q "301\|Location"; then
        log_info "✓ Redirect working (301 response)"
    else
        log_warn "⚠ Redirect path not matched"
        test_failures=$((test_failures + 1))
    fi
    
    # Test 8: Check Lua filter adds response header
    log_info "--- Test: Lua filter response header ---"
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/" "x-lua-response" "processed" "Lua filter response header" || test_failures=$((test_failures + 1))
    
    # Step 14: Verify config dump has all expected components
    log_info "Step 14: Verifying config structure..."
    local config_dump
    config_dump=$(curl -s "http://${node_ip}:${envoy_admin_port}/config_dump?include_eds" || echo "FAILED")
    
    if echo "$config_dump" | grep -qi "json_format\|jsonFormat"; then
        log_info "✓ JSON access logging configured"
    else
        log_warn "⚠ JSON access logging not found"
    fi
    
    if echo "$config_dump" | grep -qi "retry_policy\|retryPolicy"; then
        log_info "✓ Retry policies configured"
    else
        log_warn "⚠ Retry policies not found"
    fi
    
    # Step 15: Check TLS secrets
    log_info "Step 15: Checking TLS secrets (SDS)..."
    local secrets_output
    secrets_output=$(curl -s "http://${node_ip}:${envoy_admin_port}/certs" || echo "FAILED")
    
    if echo "$secrets_output" | grep -q "e2e-wildcard-cert\|e2e.local"; then
        log_info "✓ TLS secrets found in Envoy config"
    else
        log_warn "⚠ TLS secrets not found (may not be referenced by active routes)"
    fi
    
    # Step 16: Check Envoy stats
    log_info "Step 16: Checking Envoy stats..."
    local stats_output
    stats_output=$(curl -s "http://${node_ip}:${envoy_admin_port}/stats" || echo "")
    
    # Check downstream request stats
    local downstream_rq
    downstream_rq=$(echo "$stats_output" | grep "http.api-route.downstream_rq_total" | head -1 || echo "")
    if [[ -n "$downstream_rq" ]]; then
        log_info "✓ Downstream requests recorded: $downstream_rq"
    fi
    
    # Check upstream stats
    local upstream_rq
    upstream_rq=$(echo "$stats_output" | grep -E "cluster\.(complex|simple)-cluster.*upstream_rq" | head -3 || echo "")
    if [[ -n "$upstream_rq" ]]; then
        log_info "✓ Upstream request stats recorded"
        echo "$upstream_rq"
    fi
    
    # Step 17: Test SDS with Kubernetes Secret reference and live update
    log_info "Step 17: Testing SDS with Kubernetes Secret reference..."

    local sds_retries=12
    local sds_interval=5

    # Generate initial self-signed cert
    openssl req -x509 -newkey rsa:2048 -keyout /tmp/tls-initial.key -out /tmp/tls-initial.crt \
        -days 365 -nodes -subj "/CN=k8sref.e2e.local" \
        -addext "subjectAltName=DNS:k8sref.e2e.local" 2>/dev/null

    local initial_fingerprint
    initial_fingerprint=$(openssl x509 -in /tmp/tls-initial.crt -fingerprint -sha256 -noout \
        | cut -d'=' -f2 | tr -d ':' | tr '[:upper:]' '[:lower:]')
    local initial_serial
    initial_serial=$(openssl x509 -in /tmp/tls-initial.crt -serial -noout | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]')
    log_info "Initial cert fingerprint: $initial_fingerprint"
    log_info "Initial cert serial:      $initial_serial"

    # Create K8s TLS Secret
    kubectl create secret tls e2e-k8s-tls-data \
        --cert=/tmp/tls-initial.crt \
        --key=/tmp/tls-initial.key \
        -n "$NAMESPACE"

    # Create TLSSecret referencing the K8s Secret
    cat <<'TLSEOF' | kubectl apply -f -
    apiVersion: envoyxds.io/v1alpha1
    kind: TLSSecret
    metadata:
      name: e2e-k8s-ref-cert
      namespace: xds-system
      annotations:
        clusters: "e2e-test"
        nodes: "e2e-test-node"
    spec:
      domains:
        - "k8sref.e2e.local"
      config:
        type: Kubernetes
        kubernetes_config:
          secret_name: e2e-k8s-tls-data
          namespace: xds-system
TLSEOF

    # Create route using the K8s-referenced TLSSecret
    cat <<'ROUTEEOF' | kubectl apply -f -
    apiVersion: envoyxds.io/v1alpha1
    kind: Route
    metadata:
      name: k8sref-route
      namespace: xds-system
      annotations:
        clusters: "e2e-test"
        nodes: "e2e-test-node"
    spec:
      listener_refs:
        - https-complex
      tlssecret_ref: e2e-k8s-ref-cert
      filter_chain_match:
        server_names:
          - k8sref.e2e.local
      stat_prefix: k8sref-route
      codec_type: AUTO
      route_config:
        name: k8sref_route_config
        virtual_hosts:
          - name: k8sref_host
            domains:
              - "*"
            routes:
              - match:
                  prefix: /
                route:
                  timeout: 15s
                  cluster: simple-cluster
      http_filters:
        - name: envoy.filters.http.router
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
ROUTEEOF

    # Wait for reconciliation and verify initial cert
    local initial_verified=false
    for i in $(seq 1 $sds_retries); do
        local status_fp
        status_fp=$(kubectl get tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")

        if [[ -n "$status_fp" && "$status_fp" == "$initial_fingerprint" ]]; then
            log_info "✓ TLSSecret correctly loaded the K8s Secret certificate (attempt $i)"
            initial_verified=true
            break
        fi
        sleep $sds_interval
    done

    if [[ "$initial_verified" != "true" ]]; then
        log_error "✗ TLSSecret did not load initial K8s Secret certificate"
        test_failures=$((test_failures + 1))
    fi

    # Verify Envoy serves the initial cert (hard gate via HTTPS handshake)
    local served_serial_before=""
    for i in $(seq 1 $sds_retries); do
        served_serial_before=$(echo | openssl s_client -servername k8sref.e2e.local \
            -connect "${node_ip}:${envoy_https_port}" 2>/dev/null \
            | openssl x509 -serial -noout 2>/dev/null | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]' || echo "")

        if [[ "$served_serial_before" == "$initial_serial" ]]; then
            log_info "✓ Envoy is serving the initial cert (serial: $served_serial_before) (attempt $i)"
            break
        fi
        sleep $sds_interval
    done

    log_info "--- Envoy /certs BEFORE update ---"
    curl -s "http://${node_ip}:${envoy_admin_port}/certs" 2>/dev/null \
        | jq -r '.certificates[] | select(.cert_chain != null) | .cert_chain[] | select(.subject_alt_names[]?.dns? == "k8sref.e2e.local" // false)' 2>/dev/null \
        || echo "(cert not yet visible in Envoy)"
    echo "---"

    if [[ "$served_serial_before" != "$initial_serial" ]]; then
        log_error "✗ Envoy did not serve the initial certificate (expected serial: $initial_serial, got: $served_serial_before)"
        test_failures=$((test_failures + 1))
    fi

    # Generate updated cert and update K8s Secret
    openssl req -x509 -newkey rsa:2048 -keyout /tmp/tls-updated.key -out /tmp/tls-updated.crt \
        -days 365 -nodes -subj "/CN=k8sref.e2e.local/O=Updated" \
        -addext "subjectAltName=DNS:k8sref.e2e.local" 2>/dev/null

    local updated_fingerprint
    updated_fingerprint=$(openssl x509 -in /tmp/tls-updated.crt -fingerprint -sha256 -noout \
        | cut -d'=' -f2 | tr -d ':' | tr '[:upper:]' '[:lower:]')
    local updated_serial
    updated_serial=$(openssl x509 -in /tmp/tls-updated.crt -serial -noout | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]')
    log_info "Updated cert fingerprint: $updated_fingerprint"
    log_info "Updated cert serial:      $updated_serial"

    kubectl create secret tls e2e-k8s-tls-data \
        --cert=/tmp/tls-updated.crt \
        --key=/tmp/tls-updated.key \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    log_info "K8s Secret updated with new certificate"

    # Verify controller detects the change
    local update_verified=false
    for i in $(seq 1 $sds_retries); do
        local new_fp
        new_fp=$(kubectl get tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")

        if [[ "$new_fp" == "$updated_fingerprint" ]]; then
            log_info "✓ TLSSecret re-reconciled after K8s Secret update (attempt $i)"
            update_verified=true
            break
        fi
        sleep $sds_interval
    done

    if [[ "$update_verified" != "true" ]]; then
        log_error "✗ TLSSecret was NOT reconciled after K8s Secret update"
        test_failures=$((test_failures + 1))
    fi

    # Verify Envoy serves the updated cert (hard gate via HTTPS handshake)
    local served_serial_after=""
    for i in $(seq 1 $sds_retries); do
        served_serial_after=$(echo | openssl s_client -servername k8sref.e2e.local \
            -connect "${node_ip}:${envoy_https_port}" 2>/dev/null \
            | openssl x509 -serial -noout 2>/dev/null | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]' || echo "")

        if [[ "$served_serial_after" == "$updated_serial" ]]; then
            log_info "✓ Envoy is serving the updated cert (serial: $served_serial_after) (attempt $i)"
            break
        fi
        sleep $sds_interval
    done

    log_info "--- Envoy /certs AFTER update ---"
    curl -s "http://${node_ip}:${envoy_admin_port}/certs" 2>/dev/null \
        | jq -r '.certificates[] | select(.cert_chain != null) | .cert_chain[] | select(.subject_alt_names[]?.dns? == "k8sref.e2e.local" // false)' 2>/dev/null \
        || echo "(cert not visible in Envoy)"
    echo "---"

    # Summary comparison
    log_info "=== Before / After Comparison ==="
    echo "  Serial (openssl):   $initial_serial -> $updated_serial"
    echo "  Serial (Envoy TLS): $served_serial_before -> $served_serial_after"

    if [[ "$served_serial_after" != "$updated_serial" ]]; then
        log_error "✗ Envoy did not serve the updated certificate (expected serial: $updated_serial, got: $served_serial_after)"
        test_failures=$((test_failures + 1))
    else
        log_info "✓ Envoy TLS handshake confirms certificate was rotated"
    fi

    # Step 18: Force renew via annotation
    log_info "Step 18: Testing force-renew annotation (envoyxds.io/force-renew)..."

    local wildcard_fp_before=""
    for i in $(seq 1 $sds_retries); do
        wildcard_fp_before=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")
        [[ -n "$wildcard_fp_before" ]] && break
        sleep $sds_interval
    done

    if [[ -z "$wildcard_fp_before" ]]; then
        log_error "✗ e2e-wildcard-cert never reported a certificate fingerprint"
        test_failures=$((test_failures + 1))
    else
        log_info "Self-signed cert fingerprint before force-renew: $wildcard_fp_before"

        local wildcard_serial_before
        wildcard_serial_before=$(echo | openssl s_client -servername secure.e2e.local \
            -connect "${node_ip}:${envoy_https_port}" 2>/dev/null \
            | openssl x509 -serial -noout 2>/dev/null | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]' || echo "")

        kubectl annotate tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
            envoyxds.io/force-renew=true --overwrite
        log_info "force-renew annotation applied"

        local renew_verified=false
        local wildcard_fp_after=""
        for i in $(seq 1 $sds_retries); do
            wildcard_fp_after=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
                -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")

            if [[ -n "$wildcard_fp_after" && "$wildcard_fp_after" != "$wildcard_fp_before" ]]; then
                log_info "✓ force-renew issued a new certificate (attempt $i)"
                log_info "  $wildcard_fp_before -> $wildcard_fp_after"
                renew_verified=true
                break
            fi
            sleep $sds_interval
        done

        if [[ "$renew_verified" != "true" ]]; then
            log_error "✗ force-renew did not produce a new certificate"
            test_failures=$((test_failures + 1))
        fi

        # The annotation must be cleared only after the new cert is stored.
        local leftover_annotation
        leftover_annotation=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
            -o jsonpath='{.metadata.annotations.envoyxds\.io/force-renew}' 2>/dev/null || echo "")
        if [[ -z "$leftover_annotation" ]]; then
            log_info "✓ force-renew annotation was removed after success"
        else
            log_error "✗ force-renew annotation still present after a successful renewal"
            test_failures=$((test_failures + 1))
        fi

        local renewed_at
        renewed_at=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
            -o jsonpath='{.metadata.annotations.envoyxds\.io/force-renewed-at}' 2>/dev/null || echo "")
        if [[ -n "$renewed_at" ]]; then
            log_info "✓ force-renewed-at recorded: $renewed_at"
        else
            log_warn "⚠ force-renewed-at annotation not set"
        fi

        # Envoy must actually pick up the rotated certificate.
        local wildcard_serial_after=""
        for i in $(seq 1 $sds_retries); do
            wildcard_serial_after=$(echo | openssl s_client -servername secure.e2e.local \
                -connect "${node_ip}:${envoy_https_port}" 2>/dev/null \
                | openssl x509 -serial -noout 2>/dev/null | cut -d'=' -f2 | tr '[:upper:]' '[:lower:]' || echo "")

            if [[ -n "$wildcard_serial_after" && "$wildcard_serial_after" != "$wildcard_serial_before" ]]; then
                log_info "✓ Envoy is serving the force-renewed cert (serial: $wildcard_serial_after)"
                break
            fi
            sleep $sds_interval
        done

        if [[ -n "$wildcard_serial_before" && "$wildcard_serial_after" == "$wildcard_serial_before" ]]; then
            log_error "✗ Envoy still serves the pre-renewal certificate (serial: $wildcard_serial_before)"
            test_failures=$((test_failures + 1))
        fi
    fi

    # Step 19: Pause annotation
    log_info "Step 19: Testing pause annotation (envoyxds.io/pause)..."

    kubectl annotate tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
        envoyxds.io/pause=true --overwrite

    local paused_verified=false
    for i in $(seq 1 $sds_retries); do
        local paused
        paused=$(kubectl get tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.paused}' 2>/dev/null || echo "")
        if [[ "$paused" == "true" ]]; then
            log_info "✓ TLSSecret reports status.paused=true (attempt $i)"
            paused_verified=true
            break
        fi
        sleep $sds_interval
    done

    if [[ "$paused_verified" != "true" ]]; then
        log_error "✗ TLSSecret did not report status.paused after annotation"
        test_failures=$((test_failures + 1))
    fi

    # Change the underlying K8s Secret while paused; it must be ignored.
    openssl req -x509 -newkey rsa:2048 -keyout /tmp/tls-paused.key -out /tmp/tls-paused.crt \
        -days 365 -nodes -subj "/CN=k8sref.e2e.local/O=Paused" \
        -addext "subjectAltName=DNS:k8sref.e2e.local" 2>/dev/null

    local paused_fingerprint
    paused_fingerprint=$(openssl x509 -in /tmp/tls-paused.crt -fingerprint -sha256 -noout \
        | cut -d'=' -f2 | tr -d ':' | tr '[:upper:]' '[:lower:]')

    kubectl create secret tls e2e-k8s-tls-data \
        --cert=/tmp/tls-paused.crt \
        --key=/tmp/tls-paused.key \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    log_info "K8s Secret updated while paused (fingerprint: $paused_fingerprint)"

    # Give the controller several reconcile intervals to (incorrectly) pick it up.
    sleep 30

    local fp_while_paused
    fp_while_paused=$(kubectl get tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
        -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")

    if [[ "$fp_while_paused" == "$paused_fingerprint" ]]; then
        log_error "✗ Paused TLSSecret still reconciled the K8s Secret change"
        test_failures=$((test_failures + 1))
    else
        log_info "✓ Paused TLSSecret ignored the K8s Secret change"
    fi

    # Unpause and confirm it catches up.
    kubectl annotate tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" envoyxds.io/pause-
    log_info "pause annotation removed"

    local resume_verified=false
    for i in $(seq 1 $sds_retries); do
        local fp_resumed
        fp_resumed=$(kubectl get tlssecret e2e-k8s-ref-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.certificateInfo.fingerprint}' 2>/dev/null || echo "")
        if [[ "$fp_resumed" == "$paused_fingerprint" ]]; then
            log_info "✓ Reconciliation resumed after the annotation was removed (attempt $i)"
            resume_verified=true
            break
        fi
        sleep $sds_interval
    done

    if [[ "$resume_verified" != "true" ]]; then
        log_error "✗ TLSSecret did not resume after the pause annotation was removed"
        test_failures=$((test_failures + 1))
    fi

    # Step 20: Failure backoff with per-secret tuning
    log_info "Step 20: Testing failure backoff (envoyxds.io/retry-base-delay)..."

    # Local storage under /dev/null can never be read or created, so every
    # reconcile fails deterministically without any outbound network call.
    cat <<'BACKOFFEOF' | kubectl apply -f -
    apiVersion: envoyxds.io/v1alpha1
    kind: TLSSecret
    metadata:
      name: e2e-backoff-cert
      namespace: xds-system
      annotations:
        clusters: "e2e-test"
        nodes: "e2e-test-node"
      labels:
        envoyxds.io/retry-base-delay: 20s
        envoyxds.io/retry-max-delay: 5m
    spec:
      domains:
        - "backoff.e2e.local"
      config:
        type: Local
        local_config:
          path: /dev/null/certs
BACKOFFEOF

    local first_failure_verified=false
    for i in $(seq 1 $sds_retries); do
        local failures
        failures=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.failureCount}' 2>/dev/null || echo "")
        if [[ -n "$failures" && "$failures" -ge 1 ]]; then
            log_info "✓ Failure recorded: status.failureCount=$failures (attempt $i)"
            first_failure_verified=true
            break
        fi
        sleep $sds_interval
    done

    if [[ "$first_failure_verified" != "true" ]]; then
        log_error "✗ Failing TLSSecret never recorded a failure count"
        kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" -o yaml || true
        test_failures=$((test_failures + 1))
    else
        local retry_delay backoff_until
        retry_delay=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.nextRetryDelay}' 2>/dev/null || echo "")
        backoff_until=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.backoffUntil}' 2>/dev/null || echo "")

        if [[ "$retry_delay" == "20s" ]]; then
            log_info "✓ Per-secret retry-base-delay label honored (nextRetryDelay=$retry_delay)"
        else
            log_error "✗ Expected nextRetryDelay=20s from the label, got '$retry_delay'"
            test_failures=$((test_failures + 1))
        fi

        if [[ -n "$backoff_until" ]]; then
            log_info "✓ status.backoffUntil recorded: $backoff_until"
        else
            log_error "✗ status.backoffUntil was not recorded"
            test_failures=$((test_failures + 1))
        fi

        # The wait must double on the next failure.
        local escalated=false
        for i in $(seq 1 $sds_retries); do
            local next_delay
            next_delay=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
                -o jsonpath='{.status.nextRetryDelay}' 2>/dev/null || echo "")
            if [[ "$next_delay" == "40s" ]]; then
                local failures_now
                failures_now=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
                    -o jsonpath='{.status.failureCount}' 2>/dev/null || echo "")
                log_info "✓ Backoff escalated: failureCount=$failures_now, nextRetryDelay=$next_delay (attempt $i)"
                escalated=true
                break
            fi
            sleep $sds_interval
        done

        if [[ "$escalated" != "true" ]]; then
            log_error "✗ Backoff did not escalate from 20s to 40s"
            kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
                -o jsonpath='{.status.failureCount} {.status.nextRetryDelay}' || true
            echo ""
            test_failures=$((test_failures + 1))
        fi

        # A failure must not blank the reported state, and must surface as a condition.
        local error_condition
        error_condition=$(kubectl get tlssecret e2e-backoff-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.conditions[?(@.type=="Error")].status}' 2>/dev/null || echo "")
        if [[ "$error_condition" == "True" ]]; then
            log_info "✓ Error condition reported"
        else
            log_error "✗ Error condition not set on a failing TLSSecret (got '$error_condition')"
            test_failures=$((test_failures + 1))
        fi
    fi

    kubectl delete tlssecret e2e-backoff-cert -n "$NAMESPACE" --ignore-not-found

    # Step 21: Status freshness
    log_info "Step 21: Verifying status.lastReconciled keeps advancing..."

    local reconciled_before
    reconciled_before=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
        -o jsonpath='{.status.lastReconciled}' 2>/dev/null || echo "")
    log_info "lastReconciled before: $reconciled_before"

    # The controller runs with --statusRefreshInterval=20s in this environment,
    # so the timestamp must move even though renewal is a year away.
    local freshness_verified=false
    for i in $(seq 1 $sds_retries); do
        sleep $sds_interval
        local reconciled_now
        reconciled_now=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
            -o jsonpath='{.status.lastReconciled}' 2>/dev/null || echo "")
        if [[ -n "$reconciled_now" && "$reconciled_now" != "$reconciled_before" ]]; then
            log_info "✓ lastReconciled advanced: $reconciled_before -> $reconciled_now (attempt $i)"
            freshness_verified=true
            break
        fi
    done

    if [[ "$freshness_verified" != "true" ]]; then
        log_error "✗ status.lastReconciled did not advance; status would go stale"
        test_failures=$((test_failures + 1))
    fi

    local days_left
    days_left=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
        -o jsonpath='{.status.certificateInfo.daysUntilExpiry}' 2>/dev/null || echo "")
    local next_renewal
    next_renewal=$(kubectl get tlssecret e2e-wildcard-cert -n "$NAMESPACE" \
        -o jsonpath='{.status.nextRenewal}' 2>/dev/null || echo "")
    if [[ -n "$days_left" && -n "$next_renewal" ]]; then
        log_info "✓ Status reports daysUntilExpiry=$days_left nextRenewal='$next_renewal'"
    else
        log_error "✗ Status is missing daysUntilExpiry or nextRenewal"
        test_failures=$((test_failures + 1))
    fi

    # Summary
    log_info "=== E2E Test Summary ==="
    if [[ $test_failures -eq 0 ]]; then
        log_info "✓ All traffic tests passed!"
        log_info "=== E2E Tests Completed Successfully ==="
    else
        log_error "✗ $test_failures traffic test(s) failed"
        log_info "=== E2E Tests Completed with Failures ==="
        return 1
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

# Run tests
run_tests "$@"
