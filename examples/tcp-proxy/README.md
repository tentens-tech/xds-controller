# TCP Proxy Example - Layer 4 Database Proxying

This example demonstrates how to use Envoy as a TCP proxy for non-HTTP services like databases.

## What's Included

| Resource | Name | Port | Description |
| -------- | ---- | ---- | ----------- |
| Listener | `postgres-proxy` | 5432 | PostgreSQL TCP proxy |
| Listener | `redis-proxy` | 6379 | Redis TCP proxy |
| Listener | `mysql-proxy` | 3306 | MySQL TCP proxy |
| Cluster | `postgres-cluster` | - | PostgreSQL backend |
| Cluster | `redis-cluster` | - | Redis backend |
| Cluster | `mysql-cluster` | - | MySQL backend |

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-tcp

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
kubectl get listeners,clusters -n xds-system

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-tcp
```

## Architecture

```text
                    ┌─────────────────────────────────────────────────┐
                    │                    Envoy                        │
                    │                                                 │
  PostgreSQL :5432  │  ┌───────────────┐      ┌─────────────────────┐ │
 ───────────────────►  │ TCP Listener  │─────►│  postgres-cluster   │─┼──► PostgreSQL
                    │  └───────────────┘      └─────────────────────┘ │
                    │                                                 │
  Redis :6379       │  ┌───────────────┐      ┌─────────────────────┐ │
 ───────────────────►  │ TCP Listener  │─────►│   redis-cluster     │─┼──► Redis
                    │  └───────────────┘      └─────────────────────┘ │
                    │                                                 │
  MySQL :3306       │  ┌───────────────┐      ┌─────────────────────┐ │
 ───────────────────►  │ TCP Listener  │─────►│   mysql-cluster     │─┼──► MySQL
                    │  └───────────────┘      └─────────────────────┘ │
                    │                                                 │
                    └─────────────────────────────────────────────────┘
```

## Prerequisites

1. Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
2. kubectl
3. Database services running in your cluster (optional - update addresses in resources.yaml)

## Understanding TCP Proxy

TCP proxy operates at Layer 4 (Transport Layer):

- **No protocol inspection** - Raw TCP bytes are forwarded
- **Connection-based** - Tracks TCP connections, not requests
- **Health checks** - Uses TCP connection health checks
- **Load balancing** - Distributes connections across backends

### Key Differences from HTTP Proxy

| Feature | HTTP Proxy | TCP Proxy |
| ------- | ---------- | --------- |
| Layer | L7 (Application) | L4 (Transport) |
| Protocol | HTTP/HTTPS/gRPC | Any TCP |
| Routing | Path, headers, host | Port only |
| Health checks | HTTP endpoints | TCP connection |
| Use case | Web services | Databases, message queues |

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

# 4. Apply TCP proxy resources
kubectl apply -f resources.yaml
```

## Configuration

### Update Service Addresses

Edit `resources.yaml` to point to your actual services:

```yaml
# PostgreSQL
address: postgres.database.svc.cluster.local
port_value: 5432

# Redis  
address: redis.cache.svc.cluster.local
port_value: 6379

# MySQL
address: mysql.database.svc.cluster.local
port_value: 3306
```

### Expose Envoy TCP Ports

Update the Envoy Service to expose TCP ports:

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
    - name: postgres
      port: 5432
      targetPort: 5432
    - name: redis
      port: 6379
      targetPort: 6379
    - name: mysql
      port: 3306
      targetPort: 3306
```

## Test

```bash
# Port-forward Envoy
kubectl port-forward -n xds-system svc/envoy 5432:5432 6379:6379 3306:3306

# Test PostgreSQL connection
psql -h localhost -p 5432 -U postgres

# Test Redis connection
redis-cli -h localhost -p 6379 ping

# Test MySQL connection
mysql -h localhost -P 3306 -u root -p
```

## Monitoring

```bash
# Check TCP proxy stats
kubectl port-forward -n xds-system svc/envoy 9901:9901
curl http://localhost:9901/stats | grep -E "(postgres|redis|mysql)"

# Key metrics to watch:
# - envoy.tcp.postgres.downstream_cx_total - Total connections
# - envoy.tcp.postgres.downstream_cx_active - Active connections
# - envoy.tcp.postgres.downstream_cx_tx_bytes_total - Bytes sent
# - envoy.tcp.postgres.downstream_cx_rx_bytes_total - Bytes received
```

## Health Checks

TCP health checks verify backend connectivity:

```yaml
health_checks:
  - timeout: 2s
    interval: 10s
    unhealthy_threshold: 3  # Mark unhealthy after 3 failures
    healthy_threshold: 1    # Mark healthy after 1 success
    tcp_health_check: {}    # Simple TCP connection check
```

## Cleanup

```bash
# Remove TCP proxy resources
kubectl delete -f resources.yaml

# (Optional) Remove Envoy and controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-tcp
```

## Advanced: TCP Proxy with TLS Passthrough

For TLS-encrypted database connections:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: postgres-tls
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 5432
  filter_chains:
    - filters:
        - name: envoy.filters.network.tcp_proxy
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
            stat_prefix: postgres_tls
            cluster: postgres-cluster
```

## Next Steps

- [Load Balancer](../load-balancer/) - Multi-zone failover for databases
- [Advanced](../advanced/) - Circuit breakers for database protection
