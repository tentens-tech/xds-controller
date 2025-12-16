# Envoy xDS Controller

[![CI](https://github.com/tentens-tech/xds-controller/actions/workflows/ci.yml/badge.svg)](https://github.com/tentens-tech/xds-controller/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/tentens-tech/xds-controller)](https://goreportcard.com/report/github.com/tentens-tech/xds-controller)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Release](https://img.shields.io/github/v/release/tentens-tech/xds-controller)](https://github.com/tentens-tech/xds-controller/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tentens-tech/xds-controller)](https://go.dev/)

A Kubernetes-native control plane for [Envoy Proxy](https://www.envoyproxy.io/) that implements the xDS (discovery service) protocol. It enables dynamic configuration of Envoy proxies through Kubernetes Custom Resources, with built-in support for automatic TLS certificate management via Let's Encrypt.

## What is it?

The Envoy xDS Controller is an xDS control plane that translates Kubernetes Custom Resources into Envoy configurations. Instead of manually managing Envoy's complex YAML configurations, you define simple Kubernetes resources, and the controller automatically:

- **Generates and pushes configurations** to Envoy proxies via gRPC
- **Manages TLS certificates** automatically using Let's Encrypt (ACME protocol)
- **Supports multiple Envoy instances** with different configurations using cluster/node annotations
- **Provides hot-reload capability** - Envoy picks up changes without restarts

## Why do you need it?

Managing Envoy at scale presents several challenges:

| Challenge | How xDS Controller Solves It |
|-----------|------------------------------|
| **Manual configuration** | Define resources in Kubernetes CRDs, not complex Envoy YAML |
| **Certificate management** | Automatic Let's Encrypt integration with DNS-01/HTTP-01 challenges |
| **Configuration sprawl** | Single source of truth in Kubernetes |
| **Zero-downtime updates** | Hot configuration reload via xDS protocol |
| **Multi-environment support** | Cluster/node annotations for environment-specific configs |
| **Secret storage** | Integration with HashiCorp Vault or local storage |

## Key Features

- **Full xDS Protocol Support**
  - LDS (Listener Discovery Service) - Dynamic listener configuration
  - RDS (Route Discovery Service) - Dynamic routing configuration
  - CDS (Cluster Discovery Service) - Dynamic upstream cluster configuration
  - SDS (Secret Discovery Service) - Dynamic TLS certificate management
  - EDS (Endpoint Discovery Service) - Dynamic endpoint configuration

- **Automatic TLS Management**
  - Let's Encrypt integration (Staging & Production)
  - DNS-01 and HTTP-01 ACME challenges
  - Multiple DNS provider support (Cloudflare, Google Cloud DNS, etc.)
  - Automatic certificate renewal
  - Certificate expiry monitoring with Prometheus metrics

- **Advanced Capabilities**
  - Full HTTP Connection Manager (HCM) configuration support
  - QUIC/HTTP3 support
  - TCP proxy support
  - Leader election for HA deployments
  - Prometheus metrics for monitoring
  - Vault integration for secret storage

## Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                       │
│                                                                 │
│  ┌─────────────┐    ┌──────────────────────────────────────┐    │
│  │   kubectl   │──→ │     Custom Resources (CRDs)          │    │
│  │   / Helm    │    │  • Listener  • Route  • Cluster      │    │
│  └─────────────┘    │  • TLSSecret • Endpoint              │    │
│                     └──────────────────────────────────────┘    │
│                                    │                            │
│                                    ▼                            │
│                     ┌──────────────────────────────────────┐    │
│                     │       Envoy xDS Controller           │    │
│                     │  ┌────────────────────────────────┐  │    │
│                     │  │  • LDS Controller              │  │    │
│                     │  │  • RDS Controller              │  │    │
│                     │  │  • CDS Controller              │  │    │
│                     │  │  • SDS Controller (ACME)       │  │    │
│                     │  │  • EDS Controller              │  │    │
│                     │  └────────────────────────────────┘  │    │
│                     │                 │                    │    │
│                     │                 ▼                    │    │
│                     │  ┌────────────────────────────────┐  │    │
│                     │  │      xDS gRPC Server           │  │    │
│                     │  │   (Aggregated Discovery)       │  │    │
│                     │  └────────────────────────────────┘  │    │
│                     └──────────────────────────────────────┘    │
│                                    │                            │
│                          gRPC (xDS Protocol)                    │
│                                    │                            │
│                                    ▼                            │
│                     ┌──────────────────────────────────────┐    │
│                     │         Envoy Proxy Fleet            │    │
│                     │    (Configuration hot-reloaded)      │    │
│                     └──────────────────────────────────────┘    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

External Dependencies:
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Let's Encrypt  │    │  DNS Provider   │    │ HashiCorp Vault │
│     (ACME)      │    │  (Cloudflare,   │    │   (Optional)    │
│                 │    │   GCloud, etc)  │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Quick Start

### KIND (Local Demo)

Try the full xDS Controller locally with KIND:

```bash
# Prerequisites: docker, kind, kubectl
./kind/setup.sh

# Test
curl http://localhost:8080

# Cleanup
kind delete cluster --name xds-demo
```

See [kind/README.md](kind/README.md) for details.

### Deploy to Kubernetes

```bash
# 1. Install CRDs
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_listeners.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_routes.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_clusters.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_endpoints.yaml
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/crd/bases/envoyxds.io_tlssecrets.yaml

# 2. Deploy xDS Controller
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 3. Deploy Envoy (connected to xDS Controller)
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 4. Run demo (nginx + Listener + Cluster + Route)
kubectl apply -f https://raw.githubusercontent.com/tentens-tech/xds-controller/main/config/demo/demo.yaml

# 5. Test
kubectl port-forward -n xds-system svc/envoy 8080:8080
curl http://localhost:8080/
```

You should see the nginx welcome page served through Envoy! 🎉

### Local Development

#### Prerequisites

- Go 1.25+
- Kubernetes cluster (local: [KIND](https://sigs.k8s.io/kind), [minikube](https://minikube.sigs.k8s.io/))
- kubectl configured with your cluster

#### Installation

1. **Apply the Custom Resource Definitions (CRDs):**

   ```sh
   kubectl apply -f config/crd/bases/
   ```

2. **Deploy the controller:**

   ```sh
   kubectl apply -f config/controller/
   ```

3. **Apply sample configuration:**

   ```sh
   kubectl apply -f config/samples/
   ```

4. **Run the controller locally:**

   ```sh
   go run ./cmd/xds
   ```

5. **Deploy Envoy:**

   ```sh
   kubectl apply -f config/envoy/envoy-deployment.yaml
   ```

## Documentation

| Component | Description |
|-----------|-------------|
| [LDS (Listener Discovery Service)](controllers/lds/README.md) | Configure how Envoy listens for incoming connections |
| [RDS (Route Discovery Service)](controllers/rds/README.md) | Configure routing rules and virtual hosts |
| [CDS (Cluster Discovery Service)](controllers/cds/README.md) | Configure upstream clusters and load balancing |
| [EDS (Endpoint Discovery Service)](controllers/eds/README.md) | Configure dynamic endpoints for clusters |
| [SDS (Secret Discovery Service)](controllers/sds/README.md) | Manage TLS certificates with Let's Encrypt |

## Configuration

### Environment Variables

#### Core Settings

| Parameter | Description | Required |
|-----------|-------------|----------|
| `XDS_NAMESPACE` | Kubernetes namespace to watch | No (all namespaces) |
| `XDS_LOG_LEVEL` | Log level (debug, info, warn, error) | No (info) |

#### Let's Encrypt (ACME)

| Parameter | Description | Required |
|-----------|-------------|----------|
| `XDS_LETS_ENCRYPT_EMAIL` | Email for Let's Encrypt notifications | Yes (for TLS) |
| `XDS_LETS_ENCRYPT_PRIVATEKEYB64` | Base64 RSA key for Let's Encrypt account | No (auto-generated) |

#### Vault Storage (Optional)

| Parameter | Description | Required |
|-----------|-------------|----------|
| `XDS_VAULT_URL` | HashiCorp Vault URL | No |
| `XDS_VAULT_TOKEN` | Vault authentication token | No |
| `XDS_VAULT_PATH` | Vault KV2 secret path (e.g., "secret/envoy") | No |

#### DNS Provider Environment Variables (Lego)

SDS uses [lego](https://github.com/go-acme/lego) for ACME certificate management. Set the appropriate environment variables for your DNS provider:

| Provider | Environment Variables |
|----------|----------------------|
| Cloudflare | `CLOUDFLARE_DNS_API_TOKEN` |
| Google Cloud DNS | `GCE_PROJECT`, `GCE_SERVICE_ACCOUNT_FILE` |
| AWS Route53 | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` |
| DigitalOcean | `DO_AUTH_TOKEN` |

📚 **Full list:** [lego DNS Providers](https://go-acme.github.io/lego/dns/)

### Node and Cluster Targeting

The xDS Controller supports targeting specific Envoy instances using **node** and **cluster** annotations. This maps directly to Envoy's `node.id` and `node.cluster` configuration.

#### How It Works

```text
┌─────────────────────────────────────────────────────────────────────┐
│                    Envoy Configuration                               │
│                                                                      │
│  node:                                                               │
│    cluster: production    ←── matches "clusters" annotation          │
│    id: envoy-01           ←── matches "nodes" annotation             │
└─────────────────────────────────────────────────────────────────────┘
```

#### Default Behavior (Global)

**If no annotations are specified**, resources are sent to the **default node** (`global/global`):

```yaml
# No annotations = sent to all Envoy instances with node.cluster=global, node.id=global
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: http
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 8080
```

Default Envoy config to receive these resources:

```yaml
node:
  cluster: global
  id: global
```

#### Targeting Specific Envoys

Use annotations to send resources to specific Envoy instances:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: https
  annotations:
    clusters: "production"      # Target Envoys with node.cluster=production
    nodes: "envoy-01,envoy-02"  # Target specific node IDs
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 443
```

| Annotation | Description | Example |
|------------|-------------|---------|
| `clusters` | Comma-separated list of Envoy clusters to target | `"production,staging"` |
| `nodes` | Comma-separated list of Envoy node IDs to target | `"01,02,03"` |

#### Multi-Environment Example

```yaml
# Production listener - only sent to production Envoys
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: https-prod
  annotations:
    clusters: "production"
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 443
---
# Staging listener - only sent to staging Envoys  
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: https-staging
  annotations:
    clusters: "staging"
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 443
```

Production Envoy config:

```yaml
node:
  cluster: production
  id: envoy-prod-01
```

Staging Envoy config:

```yaml
node:
  cluster: staging
  id: envoy-staging-01
```

### Route References

Routes use spec fields to reference listeners and TLS certificates:

```yaml
spec:
  listener_refs:
    - "https"              # Attach route to this listener
  tlssecret_ref: "my-cert" # TLS certificate name (optional)
```

## Prometheus Metrics

| Metric | Description |
|--------|-------------|
| `xds_cert_expiry_countdown` | Minutes until certificate expiry |
| `xds_snapshot_version_match` | Config sync status (1=synced, 0=mismatch) |
| `xds_snapshot_update_total` | Total configuration updates |
| `xds_envoy_stream_active` | Active Envoy connections |
| `xds_resource_count` | Resource count by type |
| `xds_error_total` | Error counter by type |
| `xds_config_error_count` | Configuration errors |

## Quick Examples

For comprehensive examples with production-ready configurations, see the individual controller documentation:

| Resource | Documentation | Description |
|----------|---------------|-------------|
| **Listener** | [LDS Examples](controllers/lds/README.md) | HTTP, HTTPS, QUIC/HTTP3, TCP proxy |
| **Route** | [RDS Examples](controllers/rds/README.md) | Routing, CORS, Lua, gRPC, compression |
| **Cluster** | [CDS Examples](controllers/cds/README.md) | Load balancing, health checks, circuit breakers |
| **Endpoint** | [EDS Examples](controllers/eds/README.md) | Locality-aware routing, failover |
| **TLSSecret** | [SDS Examples](controllers/sds/README.md) | Let's Encrypt, Vault, self-signed |

### Minimal Working Example

```yaml
# 1. Listener - accepts traffic on port 8080
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: http
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 8080
---
# 2. Cluster - defines the backend
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: backend
spec:
  name: backend
  connect_timeout: 5s
  type: STRICT_DNS
  lb_policy: ROUND_ROBIN
  load_assignment:
    cluster_name: backend
    endpoints:
      - lb_endpoints:
          - endpoint:
              address:
                socket_address:
                  address: my-service.default.svc.cluster.local
                  port_value: 80
---
# 3. Route - connects listener to cluster
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: default-route
spec:
  listener_refs:
    - http
  stat_prefix: default
  route_config:
    virtual_hosts:
      - name: default
        domains: ["*"]
        routes:
          - match:
              prefix: /
            route:
              cluster: backend
  http_filters:
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

### HTTPS with Let's Encrypt

```yaml
# TLS Certificate (auto-managed via Let's Encrypt)
apiVersion: envoyxds.io/v1alpha1
kind: TLSSecret
metadata:
  name: my-cert
spec:
  domains:
    - "example.com"
    - "*.example.com"
  challenge:
    challenge_type: DNS01
    dns01_provider: cloudflare
    acme_env: Production
---
# HTTPS Listener
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: https
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 443
  listener_filters:
    - name: envoy.filters.listener.tls_inspector
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.listener.tls_inspector.v3.TlsInspector
---
# HTTPS Route with TLS
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: secure-route
spec:
  listener_refs:
    - https
  tlssecret_ref: my-cert
  filter_chain_match:
    server_names:
      - example.com
  stat_prefix: secure
  route_config:
    virtual_hosts:
      - name: secure
        domains: ["*"]
        routes:
          - match:
              prefix: /
            route:
              cluster: backend
  http_filters:
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

## Development

### Generate CRDs

```sh
make manifests
```

### Run Tests

```sh
make test
```

### Build

```sh
make build
```

## Built With

- [Kubebuilder](https://book.kubebuilder.io/) - Kubernetes controller framework
- [go-control-plane](https://github.com/envoyproxy/go-control-plane) - Envoy xDS implementation
- [lego](https://github.com/go-acme/lego) - ACME client for Let's Encrypt
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) - Controller utilities

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
