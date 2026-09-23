#!/bin/bash
# Route lifecycle checks against a running e2e environment: edits, no-op changes,
# conflict winner removal, invalid matchers and a controller restart.
# Usage: NODE_IP=... ADMIN_PORT=... HTTPS_PORT=... test/e2e/lifecycle.sh
set -uo pipefail

NAMESPACE="${NAMESPACE:-xds-system}"
: "${NODE_IP:?}" "${ADMIN_PORT:?}" "${HTTPS_PORT:?}"
ADMIN="http://${NODE_IP}:${ADMIN_PORT}"
FAILURES=0

pass() { echo "✅ $1"; }
fail() { echo "❌ $1"; FAILURES=$((FAILURES + 1)); }

# Prints response header $1 for an HTTPS request with SNI $2 (empty = no SNI) and path $3.
header_for_sni() {
    local name=$1 sni=$2 path=${3:-/}
    local target="https://${NODE_IP}:${HTTPS_PORT}${path}"
    local args=(-sk -D - -o /dev/null --max-time 10 --http1.1)
    if [[ -n "$sni" ]]; then
        args+=(--connect-to "${sni}:${HTTPS_PORT}:${NODE_IP}:${HTTPS_PORT}")
        target="https://${sni}:${HTTPS_PORT}${path}"
    fi
    curl "${args[@]}" "$target" 2>/dev/null | tr -d '\r' \
        | awk -v n="$name" -F': ' 'tolower($1)==n {print $2}' || true
}

# Waits until header $1 for SNI $2 equals $3; prints the last value seen.
wait_header() {
    local name=$1 sni=$2 want=$3 got=""
    for _ in $(seq 1 24); do
        got=$(header_for_sni "$name" "$sni")
        [[ "$got" == "$want" ]] && break
        sleep 5
    done
    echo "$got"
}

stat() {
    curl -s "${ADMIN}/stats" | awk -F': ' -v s="$1" '$1==s {print $2}'
}

# Everything that moves when Envoy accepts or rejects a pushed LDS or RDS update.
push_state() {
    curl -s "${ADMIN}/stats" | awk -F': ' '
        $1=="listener_manager.lds.update_success" || $1=="listener_manager.lds.update_rejected" ||
        $1=="listener_manager.lds.version" || $1=="listener_manager.listener_modified" ||
        $1 ~ /\.rds\..*\.(update_success|update_rejected)$/ {print $1"="$2}' | sort
}

# Config versions Envoy runs; a reconnect re-sends the snapshot, so update counters may move.
version_state() {
    curl -s "${ADMIN}/stats" | awk -F': ' '
        $1=="listener_manager.lds.version" || $1=="listener_manager.listener_modified" ||
        $1=="listener_manager.lds.update_rejected" ||
        $1 ~ /\.rds\..*\.(version|update_rejected)$/ {print $1"="$2}' | sort
}

route_status() {
    kubectl get route "$1" -n "$NAMESPACE" -o jsonpath="{.status.conditions[?(@.type==\"$2\")].status}" 2>/dev/null || true
}

wait_route_status() {
    local route=$1 cond=$2 want=$3 got=""
    for _ in $(seq 1 24); do
        got=$(route_status "$route" "$cond")
        [[ "$got" == "$want" ]] && break
        sleep 5
    done
    echo "$got"
}

apply_sni_route() {
    local name=$1 sni=$2 chain=$3 stat_prefix=${4:-$1}
    cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
  annotations:
    clusters: "e2e-test"
    nodes: "e2e-test-node"
spec:
  listener_refs:
    - https-complex
  tlssecret_ref: e2e-wildcard-cert
  filter_chain_match:
    server_names:
      - ${sni}
  stat_prefix: ${stat_prefix}
  codec_type: AUTO
  route_config:
    name: ${name}
    virtual_hosts:
      - name: ${name}
        domains: ["*"]
        response_headers_to_add:
          - header:
              key: x-e2e-chain
              value: "${chain}"
        routes:
          - match: {prefix: /}
            route:
              cluster: simple-cluster
              timeout: 15s
  http_filters:
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
EOF
}

echo "=== Route lifecycle ==="
rejected_start=$(stat listener_manager.lds.update_rejected)

echo "--- New route is served on its SNI ---"
apply_sni_route lifecycle-a lifecycle.e2e.local lifecycle-a
got=$(wait_header x-e2e-chain lifecycle.e2e.local lifecycle-a)
[[ "$got" == "lifecycle-a" ]] && pass "lifecycle-a serves lifecycle.e2e.local" || fail "lifecycle-a not served (got '$got')"

echo "--- Listener-level (HCM) edit is pushed ---"
apply_sni_route lifecycle-a lifecycle.e2e.local lifecycle-a lifecycle-a-v2
counter=""
for _ in $(seq 1 24); do
    header_for_sni x-e2e-chain lifecycle.e2e.local >/dev/null
    counter=$(stat http.lifecycle-a-v2.downstream_rq_total)
    [[ "$counter" =~ ^[1-9][0-9]*$ ]] && break
    sleep 5
done
[[ "$counter" =~ ^[1-9][0-9]*$ ]] && pass "stat_prefix change reached Envoy (http.lifecycle-a-v2.downstream_rq_total=$counter)" \
    || fail "stat_prefix change not applied (http.lifecycle-a-v2.downstream_rq_total='$counter')"

echo "--- Route config (RDS) edit is pushed ---"
apply_sni_route lifecycle-a lifecycle.e2e.local lifecycle-a-edited lifecycle-a-v2
got=$(wait_header x-e2e-chain lifecycle.e2e.local lifecycle-a-edited)
[[ "$got" == "lifecycle-a-edited" ]] && pass "response header edit reached Envoy" || fail "response header edit not applied (got '$got')"

echo "--- Metadata-only change pushes nothing ---"
sleep 10
before=$(push_state)
kubectl annotate route lifecycle-a https-route -n "$NAMESPACE" e2e-touch="$(date +%s)" --overwrite >/dev/null
kubectl label route lifecycle-a -n "$NAMESPACE" e2e-touch="$(date +%s)" --overwrite >/dev/null
sleep 20
after=$(push_state)
if [[ -n "$before" && "$before" == "$after" ]]; then
    pass "no LDS/RDS update after annotation and label changes"
else
    fail "annotation/label change caused a push"
    diff <(echo "$before") <(echo "$after") || true
fi

echo "--- Newer duplicate is rejected, then promoted when the winner goes away ---"
apply_sni_route lifecycle-b lifecycle.e2e.local lifecycle-b
cond=$(wait_route_status lifecycle-b Error True)
msg=$(kubectl get route lifecycle-b -n "$NAMESPACE" -o jsonpath='{.status.message}' 2>/dev/null || true)
[[ "$cond" == "True" && "$msg" == *"older route 'lifecycle-a'"* ]] && pass "lifecycle-b rejected: $msg" \
    || fail "lifecycle-b not rejected (Error='$cond', message='$msg')"
got=$(header_for_sni x-e2e-chain lifecycle.e2e.local)
[[ "$got" == "lifecycle-a-edited" ]] && pass "winner keeps serving" || fail "winner replaced (got '$got')"

kubectl delete route lifecycle-a -n "$NAMESPACE" >/dev/null
got=$(wait_header x-e2e-chain lifecycle.e2e.local lifecycle-b)
[[ "$got" == "lifecycle-b" ]] && pass "lifecycle-b promoted after lifecycle-a was deleted" || fail "lifecycle-b not promoted (got '$got')"
cond=$(wait_route_status lifecycle-b Ready True)
[[ "$cond" == "True" ]] && pass "lifecycle-b reports Ready" || fail "lifecycle-b Ready='$cond'"

echo "--- Deleted route stops serving ---"
kubectl delete route lifecycle-b -n "$NAMESPACE" >/dev/null
got=""
for _ in $(seq 1 24); do
    got=$(header_for_sni x-e2e-chain lifecycle.e2e.local)
    [[ -n "$got" && "$got" != lifecycle-* ]] && break
    sleep 5
done
[[ -n "$got" && "$got" != lifecycle-* ]] && pass "lifecycle.e2e.local falls back to the no-SNI chain ($got)" \
    || fail "deleted route still served or listener broken (got '$got')"

echo "--- Invalid filter_chain_match is reported, listener untouched ---"
apply_sni_route lifecycle-bad 'life*cycle.e2e.local' lifecycle-bad
cond=$(wait_route_status lifecycle-bad Error True)
msg=$(kubectl get route lifecycle-bad -n "$NAMESPACE" -o jsonpath='{.status.message}' 2>/dev/null || true)
[[ "$cond" == "True" && "$msg" == *"invalid filter_chain_match"* ]] && pass "invalid matcher reported: $msg" \
    || fail "invalid matcher not reported (Error='$cond', message='$msg')"
kubectl delete route lifecycle-bad -n "$NAMESPACE" >/dev/null

echo "--- QUIC listener gets the HTTP/3 codec ---"
codecs=$(curl -s "${ADMIN}/config_dump?resource=dynamic_listeners" \
    | jq -r '.configs[] | select(.name=="quic-complex") | .active_state.listener.filter_chains[].filters[].typed_config.codec_type' 2>/dev/null | sort -u | tr '\n' ' ')
[[ "$codecs" == "HTTP3 " ]] && pass "quic-complex HCMs use HTTP3" || fail "quic-complex HCM codecs: '$codecs'"

echo "--- Controller restart keeps the snapshot version ---"
sleep 10
before=$(version_state)
kubectl rollout restart deployment/xds-controller -n "$NAMESPACE" >/dev/null
kubectl rollout status deployment/xds-controller -n "$NAMESPACE" --timeout=180s >/dev/null
connected=""
for _ in $(seq 1 24); do
    connected=$(stat control_plane.connected_state)
    [[ "$connected" == "1" ]] && break
    sleep 5
done
sleep 20
after=$(version_state)
[[ "$connected" == "1" ]] && pass "Envoy reconnected to the new controller" || fail "Envoy not connected (connected_state='$connected')"
if [[ -n "$before" && "$before" == "$after" ]]; then
    pass "same LDS/RDS versions after restart, no listener modified"
else
    fail "restart changed config versions"
    diff <(echo "$before") <(echo "$after") || true
fi
got=$(header_for_sni x-e2e-test secure.e2e.local)
[[ "$got" == "https-route" ]] && pass "traffic still served after restart" || fail "https-route after restart: '$got'"

rejected_end=$(stat listener_manager.lds.update_rejected)
[[ "$rejected_start" == "$rejected_end" ]] && pass "Envoy rejected no LDS update during the run" \
    || fail "listener_manager.lds.update_rejected went ${rejected_start} -> ${rejected_end}"

echo ""
if [[ $FAILURES -eq 0 ]]; then
    echo "=== Route lifecycle: all checks passed ==="
else
    echo "=== Route lifecycle: ${FAILURES} check(s) failed ==="
    kubectl logs -l app=xds-controller -n "$NAMESPACE" --tail=60 || true
    exit 1
fi
