# EDS (Endpoint Discovery Service)

The Endpoint Discovery Service (EDS) is a component of the control-plane xDS service for Envoy that enables dynamic discovery and configuration of endpoints for Envoy proxies. EDS allows you to define and manage the specific network addresses (IP addresses, ports) of upstream service instances that Envoy can route traffic to.

[Back to Main](../../README.md)

## Features

- Dynamic endpoint configuration
- Locality-aware load balancing
- Priority-based failover
- Weighted load balancing per endpoint
- Health status management
- Named endpoints support
- Drop overload policies
- Multi-zone/region support

## Core Concepts

### ClusterLoadAssignment

The `ClusterLoadAssignment` is the main configuration object for EDS. It maps a cluster name to a set of endpoints, organized by locality.

### Locality

Locality defines the geographic or logical location of endpoints. It consists of:

- **Region**: Geographic region (e.g., "us-east-1")
- **Zone**: Availability zone within a region (e.g., "us-east-1a")
- **Sub-zone**: Further subdivision within a zone

### Priority

Endpoints can be assigned priorities (0 being highest). Envoy will only use lower priority endpoints when higher priority ones are unavailable or overloaded.

### Load Balancing Weight

Weights determine the proportion of traffic each endpoint or locality receives relative to others at the same priority level.

### Health Status

Each endpoint can have a health status that affects whether Envoy will send traffic to it:

- `UNKNOWN`: Health status not known
- `HEALTHY`: Endpoint is healthy
- `UNHEALTHY`: Endpoint has failed health checks
- `DRAINING`: Endpoint is being drained
- `TIMEOUT`: Health check timed out
- `DEGRADED`: Endpoint is healthy but degraded

## Example Configurations

[EDS Example config](../../config/samples/eds_v1alpha1_endpoint.yaml)

### Basic Endpoint Configuration

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: backend-service
  annotations:
    nodes: "01,02"
    clusters: production
spec:
  cluster_name: backend-service
  endpoints:
    - locality:
        region: us-east-1
        zone: us-east-1a
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 10.0.1.10
                port_value: 8080
          health_status: HEALTHY
          load_balancing_weight: 100
      load_balancing_weight: 100
      priority: 0
```

### Locality-Aware Load Balancing

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: geo-distributed-service
  annotations:
    nodes: "01"
    clusters: production
spec:
  cluster_name: geo-service
  endpoints:
    - locality:
        region: us-east-1
        zone: us-east-1a
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: east-1.example.com
                port_value: 8080
          load_balancing_weight: 100
      load_balancing_weight: 100
      priority: 0
    - locality:
        region: us-west-2
        zone: us-west-2a
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: west-1.example.com
                port_value: 8080
          load_balancing_weight: 100
      load_balancing_weight: 100
      priority: 0
```

### Priority-Based Failover

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: service-with-failover
  annotations:
    nodes: "01"
    clusters: production
spec:
  cluster_name: failover-service
  endpoints:
    # Primary endpoints (priority 0)
    - locality:
        zone: primary
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: primary-1.internal
                port_value: 8080
      priority: 0
    # Failover endpoints (priority 1)
    - locality:
        zone: failover
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: failover-1.internal
                port_value: 8080
      priority: 1
  policy:
    overprovisioning_factor: 140
```

### Health Check Port Configuration

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: service-with-health-port
  annotations:
    nodes: "01"
    clusters: production
spec:
  cluster_name: health-service
  endpoints:
    - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: service.internal
                port_value: 8080
            health_check_config:
              port_value: 8081  # Separate port for health checks
              hostname: service.internal
          health_status: HEALTHY
```

### Drop Overload Policy

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: service-with-drop-policy
  annotations:
    nodes: "01"
    clusters: production
spec:
  cluster_name: protected-service
  endpoints:
    - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: service.internal
                port_value: 8080
  policy:
    drop_overloads:
      - category: throttle
        drop_percentage:
          numerator: 10
          denominator: HUNDRED
    overprovisioning_factor: 140
```

## Configuration Parameters

### Required Parameters

- `metadata.name`: Unique name for the endpoint resource
- `spec.cluster_name`: Name of the cluster these endpoints belong to

### Optional Parameters

- `spec.endpoints`: List of LocalityLbEndpoints
- `spec.named_endpoints`: Map of named endpoints for reference
- `spec.policy`: ClusterLoadAssignment policy settings

### Endpoint Fields

- `address.socket_address.address`: IP address or hostname
- `address.socket_address.port_value`: Port number
- `health_status`: Health status of the endpoint
- `load_balancing_weight`: Weight for load balancing (1-128)
- `health_check_config`: Health check configuration

### Locality Fields

- `region`: Geographic region
- `zone`: Availability zone
- `sub_zone`: Sub-zone identifier

### Policy Fields

- `drop_overloads`: List of drop overload configurations
- `overprovisioning_factor`: Percentage above 100 for overprovisioning
- `endpoint_stale_after`: Duration after which endpoints are considered stale
- `weighted_priority_health`: Use weighted health for priority calculation

## Annotations

### Optional Annotations (for targeting specific Envoy instances)

- `clusters`: Comma-separated list of Envoy clusters (e.g., "production,staging")
- `nodes`: Comma-separated list of Envoy node IDs (e.g., "01,02")

> **Note:** If no annotations are specified, the endpoint is sent to the default node (`global/global`).

## Relationship with CDS

EDS works in conjunction with CDS (Cluster Discovery Service). There are three ways to configure endpoints:

1. **Inline in CDS**: Define endpoints directly in the Cluster's `load_assignment` field
2. **Reference from CDS**: Use `endpoint_refs` in Cluster to reference Endpoint CRs (recommended)
3. **Standalone EDS**: Use the native Envoy `cluster_name` field for EDS discovery (for EDS-type clusters)

### Using endpoint_refs (Recommended)

The recommended approach is to create Endpoint resources and reference them from Clusters using `endpoint_refs`:

```yaml
# Endpoint resource (reusable) - no cluster_name needed when using endpoint_refs
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: backend-zone-a
  annotations:
    clusters: "production"
    nodes: "01,02"
spec:
  # Note: cluster_name is NOT needed when referenced via endpoint_refs
  endpoints:
    - locality:
        zone: us-east-1a
      lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 10.0.1.10
                port_value: 8080
---
# Cluster references the Endpoint by name
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: backend-service
spec:
  endpoint_refs:
    - backend-zone-a
    - backend-zone-b  # Can reference multiple endpoints
  connect_timeout: 0.25s
  type: STRICT_DNS  # Use STRICT_DNS for DNS names, STATIC for IPs
  lb_policy: ROUND_ROBIN
```

Benefits of using `endpoint_refs`:

- **Cluster-centric control**: Add/remove endpoint sets by editing the Cluster
- **Reusable endpoints**: Same Endpoint resource can be used by multiple clusters
- **Automatic reconciliation**: When an Endpoint changes, all referencing Clusters are updated
- **Consistent API**: Same pattern as `listener_refs` in Route
- **Automatic waiting**: Cluster waits for Endpoints to exist before being added to snapshot

### When to use cluster_name vs endpoint_refs

| Field | Use Case | Cluster Type |
|-------|----------|--------------|
| `endpoint_refs` (in Cluster) | Reference Endpoint by CR name, merged into `load_assignment` | STATIC, STRICT_DNS, LOGICAL_DNS |
| `cluster_name` (in Endpoint) | Native Envoy EDS discovery for EDS-type clusters | EDS (with `eds_cluster_config`) |

**Using endpoint_refs (recommended):**
```yaml
# Endpoint - referenced by name, no cluster_name needed
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: my-endpoints  # This name is used in endpoint_refs
spec:
  endpoints:
    - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: backend.svc.cluster.local
                port_value: 8080
---
# Cluster references Endpoint by name
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: my-cluster
spec:
  type: STRICT_DNS
  endpoint_refs:
    - my-endpoints  # References the Endpoint CR name
```

**Using cluster_name (native EDS):**
```yaml
# Endpoint with cluster_name for EDS discovery
apiVersion: envoyxds.io/v1alpha1
kind: Endpoint
metadata:
  name: my-endpoints
spec:
  cluster_name: my-eds-cluster  # Must match Cluster's EDS service name
  endpoints:
    - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 10.0.1.10
                port_value: 8080
---
# EDS-type Cluster
apiVersion: envoyxds.io/v1alpha1
kind: Cluster
metadata:
  name: my-eds-cluster
spec:
  type: EDS
  eds_cluster_config:
    eds_config:
      ads: {}
      resource_api_version: V3
    service_name: my-eds-cluster  # Must match Endpoint's cluster_name
```

### How endpoint_refs Triggers Reconciliation

The Cluster controller watches for Endpoint changes. When an Endpoint is created or updated:

1. The watch handler finds all Clusters that reference this Endpoint via `endpoint_refs`
2. Those Clusters are queued for reconciliation
3. The Cluster reconciler fetches the Endpoint and merges its endpoints into `load_assignment`
4. The updated Cluster is sent to Envoy

```
Endpoint created/updated
        │
        ▼
┌───────────────────┐
│ Cluster Watch     │
│ handler triggered │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ Find Clusters     │
│ with endpoint_refs│
│ matching Endpoint │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ Queue Cluster     │
│ for reconcile     │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ Merge endpoints   │
│ into load_assign  │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ Update snapshot   │
│ with new Cluster  │
└───────────────────┘
```

## Best Practices

1. **Locality Configuration**
   - Always specify locality for proper zone-aware routing
   - Use consistent region/zone naming conventions
   - Configure appropriate weights for localities

2. **Priority Management**
   - Use priority 0 for primary endpoints
   - Configure failover endpoints with higher priority numbers
   - Set appropriate overprovisioning factors

3. **Health Status**
   - Set initial health status appropriately
   - Use health check configs for active health checking
   - Consider using separate health check ports

4. **Load Balancing**
   - Assign meaningful weights to endpoints
   - Balance weights across localities
   - Consider endpoint capacity when setting weights

5. **Resilience**
   - Configure drop overload policies for protection
   - Use multiple localities for redundancy
   - Set appropriate endpoint stale timeouts

## Status Fields

The Endpoint resource provides status information:

```yaml
status:
  active: true
  endpointCount: 4
  nodes: "01,02"
  clusters: "production"
  lastReconciled: "2024-01-15T10:30:00Z"
  conditions:
    - type: Ready
      status: "True"
      reason: Active
      message: "Endpoint is active in 1 snapshots with 4 endpoints"
```

## Troubleshooting

Common issues and solutions:

1. **Endpoints Not Appearing**
   - Verify `cluster_name` matches the CDS cluster name
   - Check node and cluster annotations
   - Ensure the Endpoint resource is in the correct namespace

2. **Load Balancing Issues**
   - Verify endpoint weights are set correctly
   - Check locality weights
   - Ensure health status is HEALTHY

3. **Failover Not Working**
   - Confirm priority values are set correctly
   - Check overprovisioning factor
   - Verify health checks are configured

4. **Health Check Problems**
   - Validate health check port configuration
   - Ensure health check endpoint is accessible
   - Check hostname resolution

For more detailed information on configuring endpoints, refer to the [official Envoy EDS API documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/endpoint/v3/endpoint.proto).
