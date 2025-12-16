# Multi-Environment Example - Production/Staging Targeting

This example demonstrates how to send **different xDS configurations to different Envoy instances** based on the `clusters` annotation.

> **Note:** For simple setups, a single Envoy proxy is sufficient. This example shows how to target multiple Envoy fleets (e.g., production vs staging) with different configurations using the same xDS controller.

## Files

| File | Description |
| ---- | ----------- |
| `resources.yaml` | xDS resources (Listeners, Routes, Clusters) with environment targeting |
| `backend.yaml` | Backends + Envoy proxies for production and staging |

## Architecture

```text
                      xDS Controller
                            │
              ┌─────────────┴─────────────┐
              │                           │
              ▼                           ▼
      ┌───────────────┐          ┌───────────────┐
      │   Production  │          │    Staging    │
      │    Envoys     │          │    Envoys     │
      │               │          │               │
      │  node:        │          │  node:        │
      │   cluster:    │          │   cluster:    │
      │   production  │          │   staging     │
      │   id: global  │          │   id: global  │
      └───────────────┘          └───────────────┘
              │                           │
              ▼                           ▼
      Production                    Staging
      Resources                    Resources
      (clusters:                   (clusters:
       "production")                "staging")
```

## Prerequisites

- Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
- kubectl

## Deploy

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-multi-env

# 2. Install CRDs
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml

# 3. Deploy xDS Controller
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 4. Deploy backends + Envoy proxies (production & staging)
kubectl apply -f backend.yaml

# 5. Apply xDS resources
kubectl apply -f resources.yaml

# 6. Verify resources created
kubectl get listeners,routes,clusters -n xds-system
```

## Test

### Verify Backends Directly

```bash
# Production backend - should show "PRODUCTION"
kubectl port-forward -n production svc/backend-prod 8081:80
open http://localhost:8081/

# Staging backend - should show "STAGING"
kubectl port-forward -n staging svc/backend-staging 8082:80
open http://localhost:8082/
```

### Verify Environment Targeting via Envoy

```bash
# Production Envoy - should return x-environment: production header
kubectl port-forward -n production svc/envoy 8081:8080
curl -I http://localhost:8081/
curl http://localhost:8081/  # Should show "PRODUCTION"

# Staging Envoy - should return x-environment: staging header
kubectl port-forward -n staging svc/envoy 8082:8080
curl -I http://localhost:8082/
curl http://localhost:8082/  # Should show "STAGING"
```

### Inspect Envoy Config

```bash
# Check which listeners production Envoy received
kubectl port-forward -n production svc/envoy 9901:9901
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'
```

## Cleanup

```bash
kubectl delete -f resources.yaml
kubectl delete -f backend.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml
kind delete cluster --name xds-multi-env  # if you created it
```

---

## How Targeting Works

The `clusters` annotation on xDS resources matches Envoy's `node.cluster`:

```yaml
# xDS Resource (resources.yaml)
metadata:
  annotations:
    clusters: "production"  # Sent to Envoys with node.cluster=production

# Envoy Config (in backend.yaml)
node:
  cluster: production  # Receives resources with clusters=production
  id: global
```

### Target Multiple Environments

```yaml
annotations:
  clusters: "production,staging"  # Sent to both
```

### Default Behavior

Resources without annotations go to Envoys with `node.cluster=global, node.id=global`.

## Resources Included

### Production (clusters: "production")

| Resource | Name | Description |
| -------- | ---- | ----------- |
| Listener | `http-production` | HTTP listener on port 8080 |
| Listener | `https-production` | HTTPS listener with TLS inspector |
| Cluster | `backend-production` | High availability settings |
| Route | `route-production` | Retry policies enabled |

### Staging (clusters: "staging")

| Resource | Name | Description |
| -------- | ---- | ----------- |
| Listener | `http-staging` | HTTP listener on port 8080 |
| Cluster | `backend-staging` | Relaxed settings for testing |
| Route | `route-staging` | Longer timeouts, no retries |

## Configuration Differences

| Setting | Production | Staging |
| ------- | ---------- | ------- |
| Connect timeout | 0.25s | 5s |
| Load balancing | LEAST_REQUEST | ROUND_ROBIN |
| Health check interval | 3s | 10s |
| Unhealthy threshold | 2 | 5 |
| Circuit breaker connections | 10000 | N/A |
| Route timeout | 10s | 30s |
| Retry policy | Yes | No |

## Next Steps

- [Advanced](../advanced/) - Add CORS, compression, Lua
- [HTTPS/TLS](../https-tls/) - Different TLS configs per environment
