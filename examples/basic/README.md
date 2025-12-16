# Basic Example - Minimal HTTP Proxy

This is the simplest example of using xDS Controller to configure Envoy as an HTTP proxy.

## What's Included

| Resource | Name | Description |
| -------- | ---- | ----------- |
| Listener | `http` | Accepts HTTP traffic on port 8080 |
| Cluster | `backend` | Defines the nginx backend service |
| Route | `default-route` | Routes all traffic to the backend |
| Deployment | `nginx` | Sample backend for testing |

## Architecture

```text
                    ┌─────────────┐
    HTTP :8080      │             │      ┌─────────────┐
   ─────────────────►    Envoy    ├─────►│   nginx     │
                    │             │      └─────────────┘
                    └─────────────┘
```

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-basic

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
kubectl get listeners,routes,clusters -n xds-system
kubectl port-forward -n xds-system svc/envoy 8080:8080
curl http://localhost:8080/

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f backend.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-basic
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

# 3. Deploy xDS Controller (if not already deployed)
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 4. Deploy Envoy proxy
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 5. Deploy sample backend
kubectl apply -f backend.yaml

# 6. Apply xDS resources
kubectl apply -f resources.yaml
```

## Verify

```bash
# Check xDS resources
kubectl get listeners,routes,clusters -n xds-system

# Port-forward Envoy
kubectl port-forward -n xds-system svc/envoy 8080:8080

# Test the proxy
curl http://localhost:8080/

# You should see the nginx welcome page!

# Check Envoy received the configuration
kubectl port-forward -n xds-system svc/envoy 9901:9901
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'
```

## Cleanup

```bash
# Remove xDS resources
kubectl delete -f resources.yaml

# Remove backend
kubectl delete -f backend.yaml

# (Optional) Remove Envoy and controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-basic
```

## Next Steps

- [HTTPS with TLS](../https-tls/) - Add TLS termination
- [Load Balancer](../load-balancer/) - Multi-zone load balancing
- [Advanced](../advanced/) - CORS, compression, Lua scripting
