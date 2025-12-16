# HTTPS/TLS Example - Secure Traffic with TLS Termination

This example demonstrates how to configure Envoy with TLS termination using xDS Controller.

## What's Included

| Resource | Name | Description |
| -------- | ---- | ----------- |
| TLSSecret | `dev-cert` | Self-signed certificate for development |
| Listener | `http` | HTTP on port 80 (redirect to HTTPS) |
| Listener | `https` | HTTPS on port 443 with TLS inspector |
| Cluster | `backend` | Backend service definition |
| Route | `http-redirect` | Redirects HTTP to HTTPS |
| Route | `https-route` | HTTPS route with TLS termination |

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-tls

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
kubectl apply -f ../basic/backend.yaml
kubectl apply -f resources.yaml

# 5. Verify
kubectl get listeners,routes,clusters,tlssecrets -n xds-system
kubectl port-forward -n xds-system svc/envoy 8443:443 8080:80
curl -I http://localhost:8080/       # Should redirect
curl -k https://localhost:8443/      # HTTPS with self-signed cert

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f ../basic/backend.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-tls
```

## Architecture

```text
                         ┌──────────────────────────────────────────┐
    HTTP :80             │                                          │
   ──────────────────────►  302 Redirect ──► HTTPS                  │
                         │                                          │
    HTTPS :443           │              ┌──────────┐                │
   ──────────────────────►  TLS Term ──►│  Envoy   │──► backend     │
    (TLS Termination)    │              └──────────┘                │
                         │                                          │
                         └──────────────────────────────────────────┘
```

## Prerequisites

1. Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local testing)
2. kubectl

## TLS Certificate Options

### Option 1: Self-Signed (Development)

The example uses a self-signed certificate by default:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: TLSSecret
metadata:
  name: dev-cert
spec:
  domains:
    - "*.example.local"
  config:
    type: Local
```

### Option 2: Let's Encrypt (Production)

For production, use Let's Encrypt with DNS-01 challenge:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: TLSSecret
metadata:
  name: production-cert
spec:
  domains:
    - "example.com"
    - "*.example.com"
  challenge:
    challenge_type: DNS01
    dns01_provider: cloudflare
    acme_env: Production
```

**Required Environment Variables for Let's Encrypt:**

| Provider | Variables |
| -------- | --------- |
| Cloudflare | `CLOUDFLARE_DNS_API_TOKEN` |
| AWS Route53 | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` |
| Google Cloud DNS | `GCE_PROJECT`, `GCE_SERVICE_ACCOUNT_FILE` |

Set these on the xDS Controller deployment.

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

# 4. Deploy sample backend
kubectl apply -f ../basic/backend.yaml

# 5. Apply xDS resources
kubectl apply -f resources.yaml
```

## Test

```bash
# Port-forward Envoy
kubectl port-forward -n xds-system svc/envoy 8443:443 8080:80

# Test HTTP redirect (should return 301/302)
curl -I http://localhost:8080/

# Test HTTPS (with self-signed cert, use -k to skip verification)
curl -k https://localhost:8443/

# Test with proper hostname (add to /etc/hosts: 127.0.0.1 example.local)
curl -k --resolve example.local:8443:127.0.0.1 https://example.local:8443/
```

## Verify TLS Configuration

```bash
# Check certificate details
openssl s_client -connect localhost:8443 -servername example.local </dev/null 2>/dev/null | openssl x509 -text -noout

# Check Envoy config
kubectl port-forward -n xds-system svc/envoy 9901:9901
curl http://localhost:9901/config_dump | jq '.configs[] | select(.["@type"] | contains("SecretsConfigDump"))'
```

## Cleanup

```bash
# Remove xDS resources
kubectl delete -f resources.yaml

# Remove backend
kubectl delete -f ../basic/backend.yaml

# (Optional) Remove Envoy and controller
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# (Optional) Delete kind cluster if you created one
kind delete cluster --name xds-tls
```

## Let's Encrypt Configuration

For production deployments with real certificates:

1. **Set DNS provider credentials:**

   ```bash
   kubectl set env deployment/xds-controller -n xds-system \
     CLOUDFLARE_DNS_API_TOKEN="your-api-token" \
     XDS_LETS_ENCRYPT_EMAIL="admin@example.com"
   ```

2. **Update TLSSecret to use Let's Encrypt:**

   ```yaml
   apiVersion: envoyxds.io/v1alpha1
   kind: TLSSecret
   metadata:
     name: production-cert
   spec:
     domains:
       - "example.com"
     challenge:
       challenge_type: DNS01
       dns01_provider: cloudflare
       acme_env: Production
   ```

3. **Update route to reference new certificate:**

   ```yaml
   spec:
     tlssecret_ref: production-cert
   ```

## Next Steps

- [QUIC/HTTP3](../quic-http3/) - Add HTTP/3 support
- [Advanced](../advanced/) - CORS, compression, and more
- [Multi-Environment](../multi-environment/) - Production/staging configurations
