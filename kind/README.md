# KIND Example - Full xDS Controller Setup

This example sets up a local Kubernetes cluster using KIND with the complete xDS Controller stack.

## What's Included

| Component | Description |
| --------- | ----------- |
| **KIND Cluster** | Local Kubernetes with port mappings |
| **xDS Controller** | Dynamic Envoy configuration via CRDs |
| **Envoy** | Proxy receiving config from xDS Controller |
| **Listener (LDS)** | HTTP listener on port 8080 |
| **Cluster (CDS)** | nginx backend definition |
| **Route (RDS)** | Routing rules connecting listener to cluster |
| **nginx** | Sample backend service |

## Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                        KIND Cluster                              │
│                                                                  │
│   ┌─────────────┐    ┌──────────────────┐    ┌──────────────┐   │
│   │   CRDs      │───►│  xDS Controller  │───►│    Envoy     │   │
│   │             │    │                  │    │   :30080     │───┼──► localhost:8080
│   │ - Listener  │    │  Watches CRDs    │    │   :30901     │───┼──► localhost:9901
│   │ - Route     │    │  Pushes config   │    │              │   │
│   │ - Cluster   │    │  via gRPC/xDS    │    └──────┬───────┘   │
│   └─────────────┘    └──────────────────┘           │           │
│                                                      │           │
│                                              ┌───────▼───────┐   │
│                                              │    nginx      │   │
│                                              │   (backend)   │   │
│                                              └───────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [KIND](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)

## Quick Start

```bash
# Run the setup script
chmod +x kind/setup.sh
./kind/setup.sh

# Test (no port-forward needed!)
curl http://localhost:8080

# View admin
open http://localhost:9901
```

## Manual Setup

```bash
# 1. Create KIND cluster with port mappings
kind create cluster --name xds-demo --config kind/cluster.yaml

# 2. Apply CRDs
kubectl apply -f config/crd/bases/

# 3. Deploy xDS Controller
kubectl apply -f config/controller/

# 4. Wait for controller
kubectl wait --for=condition=Available deployment/xds-controller -n xds-system --timeout=120s

# 5. Deploy Envoy
kubectl apply -f config/envoy/envoy-deployment.yaml

# 6. Apply NodePort service for direct access
kubectl apply -f kind/envoy-nodeport.yaml

# 7. Apply demo resources
kubectl apply -f config/demo/demo.yaml

# 8. Test
curl http://localhost:8080
```

## Ports

| Host Port | Service | Description |
| --------- | ------- | ----------- |
| 8080 | Envoy HTTP | Main proxy endpoint |
| 9901 | Envoy Admin | Stats, config dump, health |

## View Configuration

```bash
# List xDS resources
kubectl get listeners,routes,clusters -n xds-system

# Describe a resource
kubectl describe listener demo-http -n xds-system

# View Envoy config via admin
curl http://localhost:9901/config_dump | jq '.'

# Check listeners
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("ListenersConfigDump"))'
```

## Troubleshooting

### Port 8080 already in use

If ports 8080 or 9901 are already in use on your machine:

```bash
# Delete and recreate cluster (port mappings are set at cluster creation)
kind delete cluster --name xds-demo

# Edit kind/cluster.yaml to use different ports, e.g.:
#   hostPort: 18080
#   hostPort: 19901

# Then run setup again
./kind/setup.sh
```

### Envoy not receiving configuration

```bash
# Check controller logs
kubectl logs deployment/xds-controller -n xds-system

# Check Envoy logs
kubectl logs deployment/envoy -n xds-system

# Verify xDS resources exist
kubectl get listeners,routes,clusters -n xds-system
```

### Connection refused on localhost:8080

```bash
# Check if NodePort service is configured
kubectl get svc envoy -n xds-system

# Should show:
# NAME    TYPE       CLUSTER-IP     EXTERNAL-IP   PORT(S)
# envoy   NodePort   10.x.x.x       <none>        8080:30080/TCP,9901:30901/TCP

# If not NodePort, reapply:
kubectl apply -f kind/envoy-nodeport.yaml
```

## Cleanup

```bash
kind delete cluster --name xds-demo
```
