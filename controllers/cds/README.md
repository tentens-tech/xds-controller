# CDS (Cluster Discovery Service)

The Cluster Discovery Service (CDS) is a component of the control-plane xDS service for Envoy that enables dynamic discovery and configuration of clusters for Envoy proxies. It allows you to define and manage groups of backend services to which Envoy proxies can forward traffic.

[Back to Main](../../README.md)

## Features

- Dynamic cluster configuration
- Load balancing configuration
- Health checking
- Circuit breaking
- Outlier detection
- TLS configuration
- HTTP/2 and HTTP/3 support
- Automatic service discovery
- Custom endpoint configuration
- **Endpoint references** - Reference Endpoint CRs for dynamic endpoint management

## Core Concepts

### Clusters

A cluster is a group of similar upstream hosts that Envoy connects to. Each cluster has its own service discovery, load balancing, and health checking settings.

### Load Balancing

CDS supports various load balancing algorithms to distribute traffic across endpoints within a cluster:

- Round Robin (default)
- Least Request
- Ring Hash
- Random
- Maglev

### Health Checking

Active and passive health checking mechanisms ensure that traffic is only sent to healthy endpoints:

- HTTP/TCP health checks
- Custom health check matchers
- Interval and timeout configuration
- Healthy/unhealthy thresholds

## Example Configurations

[CDS Example config](../../config/samples/cds_v1alpha1_cluster.yaml)

### Basic Cluster Configuration

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: backend-service
  annotations:
    nodes: "01,02"
    clusters: production,staging
spec:
  connect_timeout: 0.25s
  type: STRICT_DNS
  lb_policy: ROUND_ROBIN
  load_assignment:
    cluster_name: backend-service
    endpoints:
      - lb_endpoints:
          - endpoint:
              address:
                socket_address:
                  address: backend.example.com
                  port_value: 443
```

### Cluster with Endpoint References

Instead of defining endpoints inline in the cluster's `load_assignment`, you can reference separate Endpoint CRs using `endpoint_refs`. This provides:

- **Decoupled management**: Endpoints can be defined once and reused by multiple clusters
- **Dynamic updates**: Update endpoints without modifying cluster configuration
- **Cleaner organization**: Separate endpoint pools by zone, environment, or purpose
- **Automatic waiting**: Cluster is not added to snapshot until referenced Endpoints exist (similar to Listener + Route pattern)

```yaml
# First, create Endpoint resources
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: backend-zone-a
spec:
  endpoints:
    - locality:
        zone: us-east-1a
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 10.0.1.10
                port_value: 8080
        - endpoint:
            address:
              socket_address:
                address: 10.0.1.11
                port_value: 8080
---
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: backend-zone-b
spec:
  endpoints:
    - locality:
        zone: us-east-1b
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 10.0.2.10
                port_value: 8080
---
# Then reference them from the Cluster
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: backend-service
  annotations:
    nodes: "01,02"
    clusters: production
spec:
  endpoint_refs:
    - backend-zone-a
    - backend-zone-b
  connect_timeout: 0.25s
  type: STRICT_DNS  # Use STRICT_DNS for DNS hostnames, STATIC for IP addresses
  lb_policy: ROUND_ROBIN
```

When Endpoint resources are updated, the Cluster is automatically reconciled with the new endpoints.

#### How endpoint_refs Works

The `endpoint_refs` mechanism follows the same pattern as `listener_refs` in Routes:

| Pattern         | Route + Listener                         | Cluster + Endpoint                                     |
| --------------- | ---------------------------------------- | ------------------------------------------------------ |
| Reference field | `listener_refs`                          | `endpoint_refs`                                        |
| Behavior        | Route adds filter chain to Listener      | Endpoint adds endpoints to Cluster's `load_assignment` |
| Wait behavior   | Listener waits for Routes                | Cluster waits for Endpoints                            |
| Auto-reconcile  | Route change triggers Listener reconcile | Endpoint change triggers Cluster reconcile             |

**Important behavior:**

- If a Cluster uses `endpoint_refs` only (no inline `load_assignment`) and the referenced Endpoints don't exist yet, the Cluster is **not added to the snapshot**
- Once the Endpoints are created, they trigger a Cluster re-reconciliation
- The Cluster is then added to the snapshot with the merged endpoints
- This prevents Envoy from receiving invalid clusters without endpoints

#### Cluster Types for endpoint_refs

Choose the appropriate cluster type based on your endpoint addresses:

| Cluster Type  | Address Format                                | Use Case                       |
| ------------- | --------------------------------------------- | ------------------------------ |
| `STATIC`      | IP addresses only (e.g., `10.0.1.10`)         | Known static IPs               |
| `STRICT_DNS`  | DNS hostnames (e.g., `service.namespace.svc`) | Kubernetes services, DNS names |
| `LOGICAL_DNS` | DNS hostnames                                 | Single host resolution         |

```yaml
# For IP addresses - use STATIC
spec:
  type: STATIC
  endpoint_refs:
    - endpoints-with-ips

# For DNS hostnames - use STRICT_DNS
spec:
  type: STRICT_DNS
  endpoint_refs:
    - endpoints-with-dns-names
```

#### Combining inline load_assignment with endpoint_refs

You can combine inline `load_assignment` with `endpoint_refs`. The inline endpoints serve as a base, and referenced endpoints are merged:

```yaml
spec:
  type: STRICT_DNS
  # Base endpoints (always present)
  load_assignment:
    cluster_name: backend-service
    endpoints:
      - lb_endpoints:
          - endpoint:
              address:
                socket_address:
                  address: fallback.example.com
                  port_value: 8080
  # Additional endpoints merged from Endpoint CRs
  endpoint_refs:
    - dynamic-endpoints-zone-a
    - dynamic-endpoints-zone-b
```

This pattern is useful when you want a fallback endpoint that's always available.

### Advanced Configuration Examples

#### HTTP/2 Cluster with Circuit Breaking

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: http2-service
  annotations:
    nodes: "01,02"
    clusters: production,staging
spec:
  connect_timeout: 0.25s
  type: LOGICAL_DNS
  lb_policy: LEAST_REQUEST
  http2_protocol_options: {}  # Enables HTTP/2
  circuit_breakers:
    thresholds:
      - priority: DEFAULT
        max_connections: 1000
        max_pending_requests: 1000
        max_requests: 2000
        max_retries: 3
  health_checks:
    - timeout: 1s
      interval: 5s
      unhealthy_threshold: 3
      healthy_threshold: 2
      http_health_check:
        path: "/health"
        expected_statuses:
          start: 200
          end: 299
```

#### Resilient Cluster with Health Checks, Circuit Breakers & Multi-Zone

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: resilient-cluster
spec:
  name: resilient-cluster
  connect_timeout: 0.25s
  type: STRICT_DNS
  lb_policy: ROUND_ROBIN
  ignore_health_on_host_removal: true
  upstream_connection_options:
    tcp_keepalive: {}
  health_checks:
    - timeout: 2s
      interval: 5s
      unhealthy_threshold: 5
      healthy_threshold: 1
      http_health_check:
        path: /health
  load_assignment:
    cluster_name: resilient-cluster
    endpoints:
      - lb_endpoints:
          - endpoint:
              health_check_config:
                port_value: 80
              address:
                socket_address:
                  address: backend.default.svc.cluster.local
                  port_value: 80
        locality:
          zone: zone-a
      - lb_endpoints:
          - endpoint:
              health_check_config:
                port_value: 80
              address:
                socket_address:
                  address: backend-replica.default.svc.cluster.local
                  port_value: 80
        locality:
          zone: zone-b
  circuit_breakers:
    thresholds:
      - max_connections: 2000000
        max_pending_requests: 2000000
        max_requests: 2000000
        max_retries: 3
```

#### Simple Cluster (Minimal Configuration)

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: simple-cluster
spec:
  name: simple-cluster
  connect_timeout: 5s
  type: STRICT_DNS
  lb_policy: ROUND_ROBIN
  load_assignment:
    cluster_name: simple-cluster
    endpoints:
      - lb_endpoints:
          - endpoint:
              address:
                socket_address:
                  address: backend.default.svc.cluster.local
                  port_value: 80
```

#### TLS with Outlier Detection

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: secure-service
  annotations:
    nodes: "01,02"
    cluster: production,staging
spec:
  transport_socket:
    name: envoy.transport_sockets.tls
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
      common_tls_context:
        tls_certificate_sds_secret_configs:
          - name: tls-cert ## name of your DomainConfig secret
  outlier_detection:
    consecutive_5xx: 5
    base_ejection_time: 30s
    max_ejection_percent: 50
```

## Configuration Parameters

### Required Parameters

- `metadata.name`: Unique name for the cluster
- `spec.type`: Cluster discovery type (e.g., STRICT_DNS, LOGICAL_DNS)
- `spec.connect_timeout`: Connection timeout
- `spec.lb_policy`: Load balancing policy

### Optional Parameters

- `spec.endpoint_refs`: List of Endpoint CR names to merge into load_assignment
- `spec.load_assignment`: Inline endpoint configuration (can be combined with endpoint_refs)
- `spec.http2_protocol_options`: HTTP/2 configuration
- `spec.circuit_breakers`: Circuit breaking settings
- `spec.health_checks`: Health check configuration
- `spec.outlier_detection`: Outlier detection settings
- `spec.transport_socket`: TLS/security configuration

## Annotations

### Optional Annotations (for targeting specific Envoy instances)

- `clusters`: Comma-separated list of Envoy clusters (e.g., "production,staging")
- `nodes`: Comma-separated list of Envoy node IDs (e.g., "01,02")

> **Note:** If no annotations are specified, the cluster is sent to the default node (`global/global`).

## Parameter Guidelines

- Parameters like `*_type` should be named in uppercase, as they represent types
- Boolean parameters should use actual boolean values (`true`/`false`) without quotes
- Time durations should include units (e.g., "0.25s", "5m")
- Port values should be specified as integers

## Best Practices

1. **Cluster Organization**
   - Use descriptive cluster names
   - Group related clusters logically
   - Document cluster purposes

2. **Load Balancing**
   - Choose appropriate load balancing algorithms
   - Configure proper weights for endpoints
   - Use locality-aware load balancing when applicable

3. **Health Checking**
   - Implement both active and passive health checks
   - Set appropriate timeouts and intervals
   - Use custom health check paths when needed

4. **Security**
   - Enable TLS for secure communication
   - Configure proper certificate validation
   - Implement circuit breakers

5. **Performance**
   - Enable HTTP/2 when possible
   - Configure appropriate buffer sizes
   - Set reasonable timeouts

## Troubleshooting

Common issues and solutions:

1. **Connection Issues**
   - Verify DNS resolution
   - Check connection timeouts
   - Validate TLS configuration

2. **Load Balancing Problems**
   - Confirm endpoint weights
   - Check health check status
   - Verify load balancer configuration

3. **Performance Issues**
   - Monitor circuit breaker trips
   - Check outlier detection stats
   - Validate buffer and timeout settings

For more detailed information on configuring clusters in the CDS, please refer to the [official Envoy CDS API documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/cluster/v3/cluster.proto).
