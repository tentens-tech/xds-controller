# gRPC Example - gRPC Routing and Load Balancing

This example demonstrates how to configure Envoy for gRPC services including native gRPC, gRPC-Web for browser clients, and gRPC health checking.

## What's Included

| Resource | Name | Port | Description |
|----------|------|------|-------------|
| Listener | `grpc` | 50051 | Native gRPC listener |
| Listener | `grpc-web` | 8080 | gRPC-Web listener for browsers |
| Cluster | `grpc-backend` | - | gRPC backend with HTTP/2 and health checks |
| Route | `grpc-route` | - | gRPC routing with stats |
| Route | `grpc-web-route` | - | gRPC-Web route with CORS |

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-grpc

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
kubectl apply -f resources.yaml

# 5. Verify
kubectl get listeners,routes,clusters -n xds-system
kubectl port-forward -n xds-system svc/envoy 50051:50051
grpcurl -plaintext localhost:50051 list

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-grpc
```

## Architecture

```text
                    ┌──────────────────────────────────────────────────────────────┐
                    │                         Envoy                                 │
                    │                                                               │
  gRPC :50051       │  ┌─────────┐  ┌─────────┐  ┌─────────┐                       │
  (native)         ─►  │gRPC-Web │─►│gRPC Stat│─►│ Router  │───────────────────────┼──►  gRPC
                    │  └─────────┘  └─────────┘  └─────────┘                       │    Backend
                    │                                                               │   (:50051)
  HTTP :8080        │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐          │
  (gRPC-Web)       ─►  │gRPC-Web │─►│  CORS   │─►│gRPC Stat│─►│ Router  │──────────┼──►
                    │  └─────────┘  └─────────┘  └─────────┘  └─────────┘          │
                    │                                                               │
                    └──────────────────────────────────────────────────────────────┘
```

## Key Features

### 1. HTTP/2 for gRPC

gRPC requires HTTP/2. Enable it on the cluster:

```yaml
spec:
  http2_protocol_options: {}
```

### 2. gRPC Health Checking

Use gRPC health protocol instead of HTTP:

```yaml
health_checks:
  - grpc_health_check:
      service_name: ""  # Empty = all services
```

### 3. gRPC-Web Filter

Enable browser clients to use gRPC:

```yaml
- name: envoy.filters.http.grpc_web
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.grpc_web.v3.GrpcWeb
```

### 4. gRPC Stats

Monitor gRPC method calls:

```yaml
- name: envoy.filters.http.grpc_stats
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.grpc_stats.v3.FilterConfig
    stats_for_all_methods: true
    enable_upstream_stats: true
```

### 5. gRPC-specific Routing

Route only gRPC requests:

```yaml
routes:
  - match:
      prefix: /mypackage.MyService/
      grpc: {}  # Only match gRPC requests
```

### 6. gRPC Retry Policy

Use gRPC-specific retry conditions:

```yaml
retry_policy:
  retry_on: cancelled,deadline-exceeded,unavailable
  num_retries: 3
```

## Prerequisites

1. Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
2. kubectl
3. A gRPC service to proxy to (optional - sample backend included below)

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

# 4. Update Envoy service to expose gRPC ports (see below)

# 5. Apply xDS resources
kubectl apply -f resources.yaml
```

### Expose gRPC Ports

Update the Envoy Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: envoy
  namespace: xds-system
spec:
  selector:
    app: envoy
  ports:
    - name: grpc
      port: 50051
      targetPort: 50051
    - name: grpc-web
      port: 8080
      targetPort: 8080
```

## Test

### Test with grpcurl

```bash
# Port-forward Envoy gRPC port
kubectl port-forward -n xds-system svc/envoy 50051:50051

# List services (requires reflection enabled on backend)
grpcurl -plaintext localhost:50051 list

# Call a method
grpcurl -plaintext localhost:50051 mypackage.MyService/MyMethod

# Health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

### Test gRPC-Web

```bash
# Port-forward Envoy HTTP port
kubectl port-forward -n xds-system svc/envoy 8080:8080

# Test gRPC-Web with curl (base64 encoded)
curl -X POST http://localhost:8080/mypackage.MyService/MyMethod \
  -H "Content-Type: application/grpc-web+proto" \
  -H "X-Grpc-Web: 1" \
  --data-binary @request.bin
```

### Test CORS for gRPC-Web

```bash
# Preflight request
curl -X OPTIONS http://localhost:8080/mypackage.MyService/MyMethod \
  -H "Origin: http://localhost:3000" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: content-type,x-grpc-web" \
  -I
```

## Monitoring gRPC Stats

```bash
# Check gRPC-specific stats
kubectl port-forward -n xds-system svc/envoy 9901:9901

# All gRPC stats
curl http://localhost:9901/stats | grep grpc

# Key metrics:
# - envoy.cluster.grpc-backend.grpc.mypackage.MyService.MyMethod.0
#   (success count per method)
# - envoy.cluster.grpc-backend.grpc.mypackage.MyService.MyMethod.total
#   (total calls per method)
```

## gRPC Retry Conditions

| Condition | Description |
|-----------|-------------|
| `cancelled` | Request was cancelled |
| `deadline-exceeded` | Request timed out |
| `unavailable` | Service unavailable (equivalent to HTTP 503) |
| `resource-exhausted` | Too many requests |
| `internal` | Internal error |

## Sample gRPC Backend

If you need a test gRPC service:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grpc-service
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: grpc-service
  template:
    metadata:
      labels:
        app: grpc-service
    spec:
      containers:
        - name: grpc-service
          image: grpc/java-example-hostname:latest
          ports:
            - containerPort: 50051
---
apiVersion: v1
kind: Service
metadata:
  name: grpc-service
  namespace: default
spec:
  selector:
    app: grpc-service
  ports:
    - port: 50051
      targetPort: 50051
```

## Cleanup

```bash
# Remove xDS resources
kubectl delete -f resources.yaml

# (Optional) Remove Envoy and controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-grpc
```

## Troubleshooting

### gRPC calls failing with "upstream connect error"

1. Check HTTP/2 is enabled on the cluster
2. Verify backend service is running
3. Check health check status

```bash
curl http://localhost:9901/clusters | grep grpc-backend
```

### gRPC-Web CORS errors

Ensure CORS headers are properly configured:

```yaml
cors:
  allow_origin_string_match:
    - safe_regex:
        regex: ".*"
  allow_methods: POST,GET,OPTIONS
  allow_headers: content-type,x-grpc-web,x-user-agent
```

### Timeouts

gRPC streams can be long-lived. Adjust timeouts:

```yaml
route:
  timeout: 0s  # Disable timeout for streaming
```

## Next Steps

- [HTTPS/TLS](../https-tls/) - Add TLS for secure gRPC
- [Load Balancer](../load-balancer/) - Multi-zone gRPC load balancing
- [Advanced](../advanced/) - Add logging and metrics
