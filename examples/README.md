# Envoy xDS Controller Examples

This directory contains example configurations for the Envoy xDS Controller. Each example is self-contained and includes all necessary YAML files and documentation.

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-example

# 2. Install CRDs
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml

# 3. Deploy xDS Controller
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 4. Deploy Envoy
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 5. Try an example
cd basic && kubectl apply -f .

# 6. Verify
kubectl get listeners,routes,clusters -n xds-system
kubectl port-forward -n xds-system svc/envoy 8080:8080
curl http://localhost:8080/

# 7. Cleanup
kubectl delete -f .
cd ..
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 8. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-example
```

## Examples Overview

| Example | Description |
| ------- | ----------- |
| [basic](./basic/) | Minimal HTTP proxy setup |
| [https-tls](./https-tls/) | HTTPS with TLS termination |
| [tcp-proxy](./tcp-proxy/) | TCP proxy for databases |
| [load-balancer](./load-balancer/) | Multi-zone load balancing with failover |
| [advanced](./advanced/) | CORS, compression, Lua scripting |
| [multi-environment](./multi-environment/) | Production/staging targeting |
| [grpc](./grpc/) | gRPC routing and gRPC-Web |

## Example Details

### Basic

**[basic/](./basic/)** - The simplest possible configuration

- HTTP listener on port 8080
- Single backend cluster
- Basic routing

**Use case:** Getting started, understanding the basics

```bash
cd basic
kubectl apply -f backend.yaml
kubectl apply -f resources.yaml
```

---

### HTTPS/TLS

**[https-tls/](./https-tls/)** - Secure traffic with TLS termination

- Self-signed certificates (development)
- Let's Encrypt integration (production)
- HTTP to HTTPS redirect
- TLS inspector configuration

**Use case:** Production HTTPS setup, automatic certificate management

```bash
cd https-tls
kubectl apply -f resources.yaml
```

---

### TCP Proxy

**[tcp-proxy/](./tcp-proxy/)** - Layer 4 proxying for non-HTTP services

- PostgreSQL proxy (port 5432)
- Redis proxy (port 6379)
- MySQL proxy (port 3306)
- TCP health checks

**Use case:** Database proxying, message queue proxying, any TCP service

```bash
cd tcp-proxy
kubectl apply -f resources.yaml
```

---

### Load Balancer

**[load-balancer/](./load-balancer/)** - Advanced load balancing with failover

- Multi-zone endpoint distribution
- Priority-based failover
- Health checks and circuit breakers
- Endpoint references (`endpoint_refs`)
- Outlier detection

**Use case:** High availability, multi-region deployments, disaster recovery

```bash
cd load-balancer
kubectl apply -f backend.yaml
kubectl apply -f resources.yaml
```

---

### Advanced

**[advanced/](./advanced/)** - Full-featured HTTP configuration

- CORS configuration
- Brotli compression
- Lua scripting
- JSON access logging
- Custom headers
- Path rewriting
- Redirects
- Retry policies

**Use case:** Production API gateways, complex routing requirements

```bash
cd advanced
kubectl apply -f resources.yaml
```

---

### Multi-Environment

**[multi-environment/](./multi-environment/)** - Production/staging targeting

- Cluster annotations for environment targeting
- Node annotations for specific Envoy targeting
- Canary deployments
- Shared resources across environments
- Different configs per environment

**Use case:** Multiple environments, canary testing, gradual rollouts

```bash
cd multi-environment
kubectl apply -f resources.yaml
```

---

### gRPC

**[grpc/](./grpc/)** - gRPC routing and load balancing

- Native gRPC on port 50051
- gRPC-Web for browser clients
- gRPC health checking
- gRPC stats and monitoring
- HTTP/2 configuration

**Use case:** Microservices, browser gRPC clients, gRPC health monitoring

```bash
cd grpc
kubectl apply -f resources.yaml
```

## Prerequisites

- Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
- kubectl

## Common Tasks

### Create kind Cluster (Optional)

```bash
# Create a kind cluster for testing
kind create cluster --name xds-example

# Delete when done
kind delete cluster --name xds-example
```

### Apply CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml
```

### View Resources

```bash
# List all xDS resources
kubectl get listeners,routes,clusters,endpoints,tlssecrets -n xds-system

# Describe a specific resource
kubectl describe listener http -n xds-system
```

### Check Envoy Configuration

```bash
# Port-forward to Envoy admin
kubectl port-forward -n xds-system svc/envoy 9901:9901

# View listeners
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'

# View clusters
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ClustersConfigDump"))'

# View routes
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("RoutesConfigDump"))'

# View stats
curl http://localhost:9901/stats
```

### Test Traffic

```bash
# Port-forward to Envoy
kubectl port-forward -n xds-system svc/envoy 8080:8080

# Test HTTP
curl http://localhost:8080/

# Test with headers
curl -I http://localhost:8080/
```

### Cleanup

```bash
# Remove all xDS resources
kubectl delete listeners,routes,clusters,endpoints,tlssecrets --all -n xds-system

# Remove xDS Controller and Envoy
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# Remove CRDs (careful - removes all resources!)
kubectl delete -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/

# Delete kind cluster (optional - if you created one)
kind delete cluster --name xds-example
```

## Resource Relationships

```text
┌─────────────────────────────────────────────────────────────────────────┐
│                        xDS Resource Relationships                       │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│   ┌──────────────┐                                                      │
│   │   Listener   │  Accepts incoming traffic on a port                  │
│   └──────┬───────┘                                                      │
│          │                                                              │
│          │ listener_refs                                                │
│          ▼                                                              │
│   ┌──────────────┐                                                      │
│   │    Route     │  Defines routing rules and HTTP filters              │
│   └──────┬───────┘                                                      │
│          │                                                              │
│          │ cluster (in route config)      tlssecret_ref                 │
│          ▼                                      │                       │
│   ┌──────────────┐                              │                       │
│   │   Cluster    │  Defines backend services    │                       │
│   └──────┬───────┘                              │                       │
│          │                                      ▼                       │
│          │ endpoint_refs              ┌──────────────┐                  │
│          ▼                            │  TLSSecret   │                  │
│   ┌──────────────┐                    │              │                  │
│   │   Endpoint   │                    │ Certificates │                  │
│   │              │                    └──────────────┘                  │
│   │ Backend IPs  │                                                      │
│   └──────────────┘                                                      │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

## Annotations Reference

| Annotation | Description | Example |
| ---------- | ----------- | ------- |
| `clusters` | Target Envoy clusters | `"production,staging"` |
| `nodes` | Target specific Envoy nodes | `"envoy-01,envoy-02"` |

**Default behavior:** Resources without annotations are sent to `global/global`

## Need Help?

- [Main Documentation](../README.md)
- [LDS (Listeners)](../controllers/lds/README.md)
- [RDS (Routes)](../controllers/rds/README.md)
- [CDS (Clusters)](../controllers/cds/README.md)
- [EDS (Endpoints)](../controllers/eds/README.md)
- [SDS (TLS Secrets)](../controllers/sds/README.md)

## Contributing

Found an issue or want to add a new example? Contributions are welcome!

1. Fork the repository
2. Create a new example directory
3. Add resources.yaml and README.md
4. Submit a pull request
