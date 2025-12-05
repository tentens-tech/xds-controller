# Multi-Environment Example - Production/Staging Targeting

This example demonstrates how to send different xDS configurations to different Envoy instances based on cluster and node annotations.

## What's Included

### Production Resources (clusters: "production")

| Resource | Name | Description |
|----------|------|-------------|
| Listener | `http-production` | HTTP listener with production settings |
| Listener | `https-production` | HTTPS listener with TLS inspector |
| Cluster | `backend-production` | High availability cluster settings |
| Route | `route-production` | Production route with retry policies |

### Staging Resources (clusters: "staging")

| Resource | Name | Description |
|----------|------|-------------|
| Listener | `http-staging` | HTTP listener with staging settings |
| Cluster | `backend-staging` | Relaxed cluster settings for testing |
| Route | `route-staging` | Staging route with longer timeouts |

### Shared Resources (clusters: "production,staging")

| Resource | Name | Description |
|----------|------|-------------|
| Cluster | `metrics-backend` | Shared metrics cluster for both envs |

### Canary Resources (clusters: "production", nodes: "envoy-canary-*")

| Resource | Name | Description |
|----------|------|-------------|
| Listener | `http-canary` | Listener only for canary nodes |
| Route | `route-canary` | Canary route with experimental features |

## Quick Start

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

# 4. Apply example
kubectl apply -f resources.yaml

# 5. Verify
kubectl get listeners,routes,clusters -n xds-system

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-multi-env
```

## Architecture

```text
                    xDS Controller
                         │
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
    ┌───────────┐  ┌───────────┐  ┌───────────┐
    │ Production│  │  Staging  │  │  Canary   │
    │  Envoys   │  │  Envoys   │  │  Envoys   │
    │           │  │           │  │           │
    │ cluster:  │  │ cluster:  │  │ cluster:  │
    │ production│  │ staging   │  │ production│
    │           │  │           │  │ nodes:    │
    │           │  │           │  │ canary-*  │
    └───────────┘  └───────────┘  └───────────┘
         │              │              │
         ▼              ▼              ▼
    Production      Staging        Canary
    Resources      Resources      Resources
```

## How Targeting Works

### Cluster Annotation

The `clusters` annotation matches against Envoy's `node.cluster` configuration:

```yaml
# xDS Resource
metadata:
  annotations:
    clusters: "production"  # Sent to Envoys with node.cluster=production

# Envoy Config
node:
  cluster: production  # Receives resources with clusters=production
```

### Node Annotation

The `nodes` annotation matches against Envoy's `node.id`:

```yaml
# xDS Resource
metadata:
  annotations:
    nodes: "envoy-01,envoy-02"  # Only these specific nodes

# Envoy Config
node:
  id: envoy-01  # Receives this resource
```

### Combined Targeting

You can combine both for precise targeting:

```yaml
metadata:
  annotations:
    clusters: "production"           # Must be in production cluster
    nodes: "envoy-canary-01"         # AND must be this specific node
```

### Default Behavior

Resources without annotations go to `global/global`:

```yaml
# No annotations = sent to Envoys with node.cluster=global, node.id=global
metadata:
  name: my-listener
spec:
  ...
```

## Prerequisites

1. Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
2. kubectl

## Deploy

### 1. Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml
```

### 2. Deploy xDS Controller

```bash
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml
```

### 2. Deploy Production Envoy

```bash
# Create production Envoy ConfigMap
kubectl create configmap envoy-prod-config \
  --from-file=envoy.yaml=envoy-configs/production-envoy.yaml \
  -n production

# Deploy production Envoy (update deployment to use the config)
```

### 3. Deploy Staging Envoy

```bash
# Create staging Envoy ConfigMap
kubectl create configmap envoy-staging-config \
  --from-file=envoy.yaml=envoy-configs/staging-envoy.yaml \
  -n staging

# Deploy staging Envoy (update deployment to use the config)
```

### 4. Apply xDS Resources

```bash
kubectl apply -f resources.yaml
```

## Test

### Verify Production Envoy Receives Production Config

```bash
# Port-forward to production Envoy
kubectl port-forward -n production svc/envoy 8080:8080

# Test - should see x-environment: production header
curl -I http://localhost:8080/
```

### Verify Staging Envoy Receives Staging Config

```bash
# Port-forward to staging Envoy
kubectl port-forward -n staging svc/envoy 8080:8080

# Test - should see x-environment: staging header
curl -I http://localhost:8080/
```

### Check Which Resources Each Envoy Has

```bash
# Production Envoy config dump
kubectl port-forward -n production svc/envoy 9901:9901
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'

# Staging Envoy config dump
kubectl port-forward -n staging svc/envoy 9901:9901
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'
```

## Production vs Staging Differences

| Setting | Production | Staging |
|---------|------------|---------|
| Connect timeout | 0.25s | 5s |
| Load balancing | LEAST_REQUEST | ROUND_ROBIN |
| Health check interval | 3s | 10s |
| Unhealthy threshold | 2 | 5 |
| Circuit breaker connections | 10000 | N/A |
| Route timeout | 10s | 30s |
| Retry policy | Yes | No |

## Use Cases

### 1. Environment Isolation

Keep production and staging configurations completely separate:

```yaml
# Production-only resource
annotations:
  clusters: "production"
```

### 2. Canary Deployments

Test new configurations on specific nodes:

```yaml
# Send to specific canary nodes only
annotations:
  clusters: "production"
  nodes: "envoy-canary-01,envoy-canary-02"
```

### 3. Gradual Rollout

Expand to more nodes as confidence grows:

```yaml
# Day 1: Canary
nodes: "envoy-canary-01"

# Day 2: More nodes
nodes: "envoy-canary-01,envoy-canary-02,envoy-canary-03"

# Day 3: All production (remove nodes annotation)
clusters: "production"
```

### 4. Shared Services

Deploy common resources to multiple environments:

```yaml
# Sent to both production and staging
annotations:
  clusters: "production,staging"
```

## Cleanup

```bash
# Remove xDS resources
kubectl delete -f resources.yaml

# Remove environment-specific Envoys
kubectl delete deployment envoy -n production
kubectl delete deployment envoy -n staging

# (Optional) Remove xDS Controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-multi-env
```

## Next Steps

- [Advanced](../advanced/) - Add CORS, compression, Lua
- [HTTPS/TLS](../https-tls/) - Different TLS configs per environment
