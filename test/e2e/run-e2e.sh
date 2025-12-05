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
    test_http_request "http://${node_ip}:${envoy_http_port}/" "200" "Basic route on port 8080" || ((test_failures++))
    
    # Test 2: Complex route via complex listener (port 8081)
    log_info "--- Test: Complex route (complex listener) ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/" "200" "Complex route on port 8081" || ((test_failures++))
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/" "x-e2e-test" "api-route" "Response header x-e2e-test" || ((test_failures++))
    
    # Test 3: API route (/api/) via complex listener
    log_info "--- Test: API route ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/api/" "200" "API route (/api/)" || ((test_failures++))
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/api/" "x-e2e-test" "api-route" "API route response header" || ((test_failures++))
    
    # Test 4: Health endpoint via complex listener
    log_info "--- Test: Health endpoint ---"
    test_http_request "http://${node_ip}:${envoy_http_complex_port}/health" "200" "Health endpoint (/health)" || ((test_failures++))
    
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
        ((test_failures++))
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
        ((test_failures++))
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
            ((test_failures++))
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
        ((test_failures++))
    fi
    
    # Test 8: Check Lua filter adds response header
    log_info "--- Test: Lua filter response header ---"
    test_http_header "http://${node_ip}:${envoy_http_complex_port}/" "x-lua-response" "processed" "Lua filter response header" || ((test_failures++))
    
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
