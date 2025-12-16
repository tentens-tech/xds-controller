# Load Balancer Example - Multi-Zone with Failover

This example demonstrates advanced load balancing features including multi-zone distribution, priority-based failover, health checks, and circuit breakers.

## What's Included

| Resource | Name | Description |
| -------- | ---- | ----------- |
| Endpoint | `backend-zone-a` | Primary endpoints in zone A (priority 0) |
| Endpoint | `backend-zone-b` | Primary endpoints in zone B (priority 0) |
| Endpoint | `backend-zone-c` | Failover endpoints in zone C (priority 1) |
| Cluster | `load-balanced-backend` | Cluster with endpoint_refs, health checks, circuit breakers |
| Listener | `http` | HTTP listener on port 8080 |
| Route | `load-balanced-route` | Route with retry policy |

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-lb

# 2. Install CRDs
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml

# 3. Deploy xDS Controller and Envoy
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 4. Apply example
kubectl apply -f backend.yaml
kubectl apply -f resources.yaml

# 5. Verify
kubectl get listeners,routes,clusters,endpoints -n xds-system
kubectl wait --for=condition=ready pod -l app=backend -n xds-system --timeout=60s
kubectl port-forward -n xds-system svc/envoy 8080:8080
for i in {1..10}; do curl -s http://localhost:8080/ | head -1; done

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f backend.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-lb
```

## Architecture

```text
                                      ┌─────────────────────────────────────────┐
                                      │            Cluster Config               │
                                      │  - Health checks (HTTP /health)         │
                                      │  - Circuit breakers                     │
                                      │  - Outlier detection                    │
                                      └─────────────────────────────────────────┘
                                                         │
                                                         ▼
    ┌──────────┐     ┌──────────┐     ┌─────────────────────────────────────────┐
    │  Client  │────►│  Envoy   │────►│         Load Balancer (ROUND_ROBIN)     │
    └──────────┘     └──────────┘     └─────────────────────────────────────────┘
                                                         │
                          ┌──────────────────────────────┼──────────────────────────────┐
                          │                              │                              │
                          ▼                              ▼                              ▼
               ┌─────────────────────┐       ┌─────────────────────┐       ┌────────────────────┐
               │    Zone A (Pri 0)   │       │    Zone B (Pri 0)   │       │    Zone C (Pri 1)  │
               │  ┌───────┐ ┌───────┐│       │  ┌───────┐ ┌───────┐│       │     ┌───────┐      │
               │  │ be-a-1│ │ be-a-2││       │  │ be-b-1│ │ be-b-2││       │     │ be-c-1│      │
               │  └───────┘ └───────┘│       │  └───────┘ └───────┘│       │     └───────┘      │
               │    us-east-1a       │       │    us-east-1b       │       │    us-west-2a      │
               └─────────────────────┘       └─────────────────────┘       └────────────────────┘
                    PRIMARY ZONE                 PRIMARY ZONE                 FAILOVER ZONE
```

## Key Features

### 1. Endpoint References (`endpoint_refs`)

Instead of inline endpoints, use separate Endpoint resources:

```yaml
spec:
  endpoint_refs:
    - backend-zone-a
    - backend-zone-b
    - backend-zone-c
```

**Benefits:**

- Endpoints can be updated independently
- Same endpoints can be referenced by multiple clusters
- Cleaner separation of concerns

### 2. Priority-Based Failover

```yaml
# Primary zones (priority 0)
priority: 0

# Failover zone (priority 1)
priority: 1
```

Traffic flows to priority 0 endpoints. Only when they're unhealthy does traffic go to priority 1.

### 3. Health Checks

```yaml
health_checks:
  - timeout: 2s
    interval: 5s
    unhealthy_threshold: 3  # 3 failures = unhealthy
    healthy_threshold: 1    # 1 success = healthy
    http_health_check:
      path: /health
```

### 4. Circuit Breakers

```yaml
circuit_breakers:
  thresholds:
    - priority: DEFAULT
      max_connections: 1000
      max_pending_requests: 1000
      max_requests: 2000
      max_retries: 3
```

### 5. Outlier Detection

Automatically ejects unhealthy endpoints:

```yaml
outlier_detection:
  consecutive_5xx: 5        # 5 consecutive 5xx = ejection
  base_ejection_time: 30s   # Ejected for 30s
  max_ejection_percent: 50  # Max 50% of endpoints can be ejected
```

## Prerequisites

1. Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
2. kubectl

## Deploy

```bash
# 1. Create namespace (if not exists)
kubectl create namespace xds-system

# 2. Install CRDs
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml

# 3. Deploy xDS Controller and Envoy (if not already deployed)
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 4. Deploy multi-zone backends
kubectl apply -f backend.yaml

# 5. Apply xDS resources
kubectl apply -f resources.yaml

# 6. Wait for backends to be ready
kubectl wait --for=condition=ready pod -l app=backend -n xds-system --timeout=60s
```

## Test

### Basic Load Balancing

```bash
# Port-forward Envoy
kubectl port-forward -n xds-system svc/envoy 8080:8080

# Make multiple requests and observe distribution
for i in {1..10}; do curl -s http://localhost:8080/ | head -1; done
```

### Test Failover

```bash
# Scale down primary zone A
kubectl scale deployment backend-a-1 backend-a-2 --replicas=0 -n xds-system

# Traffic should automatically route to zone B
curl http://localhost:8080/

# Scale down zone B - traffic should go to failover zone C
kubectl scale deployment backend-b-1 backend-b-2 --replicas=0 -n xds-system
curl http://localhost:8080/

# Restore all backends
kubectl scale deployment backend-a-1 backend-a-2 backend-b-1 backend-b-2 --replicas=1 -n xds-system
```

### Monitor Health Status

```bash
# Check cluster health
kubectl port-forward -n xds-system svc/envoy 9901:9901
curl http://localhost:9901/clusters | grep -E "(load-balanced-backend|health_flags)"

# Check endpoint status
curl http://localhost:9901/clusters | grep -E "cx_active|rq_total"
```

## Load Balancing Policies

Change the `lb_policy` in the cluster spec:

| Policy | Description |
| ------ | ----------- |
| `ROUND_ROBIN` | Rotates through endpoints (default) |
| `LEAST_REQUEST` | Sends to endpoint with fewest active requests |
| `RANDOM` | Random selection |
| `RING_HASH` | Consistent hashing for session affinity |
| `MAGLEV` | Consistent hashing with better distribution |

```yaml
spec:
  lb_policy: LEAST_REQUEST
```

## Cleanup

```bash
# Remove xDS resources
kubectl delete -f resources.yaml

# Remove backends
kubectl delete -f backend.yaml

# (Optional) Remove Envoy and controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-lb
```

## Next Steps

- [Advanced](../advanced/) - Add CORS, compression, custom headers
- [Multi-Environment](../multi-environment/) - Different configs for prod/staging
