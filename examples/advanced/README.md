# Advanced Example - Full-Featured HTTP Configuration

This example demonstrates advanced HTTP proxy features including CORS, compression, Lua scripting, access logging, and more.

## What's Included

| Resource | Name | Description |
|----------|------|-------------|
| Listener | `http-advanced` | HTTP listener on port 8080 |
| Cluster | `api-backend` | Backend with health checks and circuit breakers |
| Route | `advanced-route` | Full-featured route configuration |

## Quick Start

```bash
# 1. Create kind cluster (optional - skip if you have a cluster)
kind create cluster --name xds-advanced

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
kubectl get listeners,routes,clusters -n xds-system
kubectl port-forward -n xds-system svc/envoy 8080:8080
curl http://localhost:8080/

# 6. Cleanup
kubectl delete -f resources.yaml
kubectl delete -f ../basic/backend.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml
kubectl delete -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml

# 7. Delete cluster (optional - if you created it in step 1)
kind delete cluster --name xds-advanced
```

## Features Demonstrated

### 1. Full HTTP Connection Manager (HCM) Configuration

The Route CRD supports all Envoy HCM options:

```yaml
spec:
  stat_prefix: my-route
  codec_type: AUTO
  stream_error_on_invalid_http_message: true
  normalize_path: true
  merge_slashes: true
  # Timeouts
  stream_idle_timeout: 300s
  request_timeout: 60s
  request_headers_timeout: 10s
  # HTTP/2 settings
  http2_protocol_options:
    max_concurrent_streams: 100
    initial_stream_window_size: 65536
  # Common HTTP options
  common_http_protocol_options:
    idle_timeout: 900s
  # And many more...
```

### 2. CORS (Cross-Origin Resource Sharing)

```yaml
cors:
  allow_credentials: true
  allow_origin_string_match:
    - safe_regex:
        regex: ".*"
  allow_methods: GET,POST,PUT,DELETE,PATCH,OPTIONS,HEAD
  allow_headers: Authorization,Content-Type,X-Requested-With
  max_age: 24h
```

### 3. Brotli Compression

```yaml
- name: envoy.filters.http.compressor
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
    compressor_library:
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.compression.brotli.compressor.v3.Brotli
        quality: 4
```

### 4. Lua Scripting

```yaml
- name: envoy.filters.http.lua
  typed_config:
    inline_code: |
      function envoy_on_request(request_handle)
        request_handle:headers():add("x-request-timestamp", os.time())
      end
```

### 5. JSON Access Logging

```yaml
access_log:
  - typed_config:
      "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
      path: /dev/stdout
      log_format:
        json_format:
          timestamp: "%START_TIME%"
          method: "%REQ(:METHOD)%"
          ...
```

### 6. Path Rewriting

```yaml
routes:
  - match:
      prefix: /api/v1/
    route:
      prefix_rewrite: /  # /api/v1/users -> /users
```

### 7. Redirects

```yaml
routes:
  - match:
      prefix: /old-api/
    redirect:
      path_redirect: /api/v1/
      response_code: MOVED_PERMANENTLY
```

### 8. Retry Policies

```yaml
retry_policy:
  retry_on: connect-failure,refused-stream,unavailable
  num_retries: 3
  per_try_timeout: 5s
```

### 9. Custom Headers

```yaml
response_headers_to_add:
  - header:
      key: x-api-version
      value: "v1.0"
request_headers_to_add:
  - header:
      key: x-route-name
      value: api-v1
```

## Architecture

```text
                    ┌──────────────────────────────────────────────────────────────┐
                    │                         Envoy                                │
                    │                                                              │
    HTTP :8080      │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐          │
   ─────────────────►  │  CORS   │─►│Compress │─►│   Lua   │─►│ Router  │──────────┼──► Backend
                    │  └─────────┘  └─────────┘  └─────────┘  └─────────┘          │
                    │                                                              │
                    │  Routes:                                                     │
                    │    /api/v1/*  → rewrite to /* → api-backend                  │
                    │    /health    → api-backend                                  │
                    │    /old-api/* → 301 redirect to /api/v1/*                    │
                    │    /*         → api-backend                                  │
                    │                                                              │
                    └──────────────────────────────────────────────────────────────┘
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

# 3. Deploy xDS Controller and Envoy (if not already deployed)
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/xds-controller.yaml
kubectl apply -f https://github.com/tentens-tech/xds-controller/releases/latest/download/envoy.yaml

# 4. Deploy sample backend
kubectl apply -f ../basic/backend.yaml

# 5. Apply xDS resources
kubectl apply -f resources.yaml
```

## Test

### Basic Request

```bash
kubectl port-forward -n xds-system svc/envoy 8080:8080

# Simple request
curl http://localhost:8080/

# Check response headers (custom headers from Lua)
curl -I http://localhost:8080/
```

### Test CORS

```bash
# Preflight request
curl -X OPTIONS http://localhost:8080/api/v1/users \
  -H "Origin: http://example.com" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: Content-Type" \
  -I

# Check CORS headers in response
```

### Test Compression

```bash
# Request with Accept-Encoding
curl http://localhost:8080/ \
  -H "Accept-Encoding: br, gzip" \
  --compressed \
  -v

# Check Content-Encoding header in response
```

### Test Path Rewriting

```bash
# /api/v1/users should be rewritten to /users
curl http://localhost:8080/api/v1/test
```

### Test Redirects

```bash
# Should return 301 redirect
curl -I http://localhost:8080/old-api/users
# Location: /api/v1/users
```

### Test Custom Headers

```bash
# Check request/response headers
curl http://localhost:8080/ -v 2>&1 | grep -E "^< x-"
# Should see x-api-version, x-powered-by, x-lua-processed, etc.
```

## Monitoring Access Logs

```bash
# Watch Envoy logs for JSON access logs
kubectl logs -f deployment/envoy -n xds-system | jq '.'

# Filter for errors only (status >= 400)
kubectl logs deployment/envoy -n xds-system | jq 'select(.response_code >= 400)'
```

## Available Envoy Variables for Access Logs

| Variable | Description |
|----------|-------------|
| `%START_TIME%` | Request start time |
| `%DURATION%` | Total request duration |
| `%REQUEST_DURATION%` | Time until first upstream byte |
| `%RESPONSE_CODE%` | HTTP response code |
| `%BYTES_SENT%` | Bytes sent to client |
| `%BYTES_RECEIVED%` | Bytes received from client |
| `%UPSTREAM_HOST%` | Upstream host address |
| `%UPSTREAM_CLUSTER%` | Upstream cluster name |
| `%REQ(header)%` | Request header value |
| `%RESP(header)%` | Response header value |

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
kind delete cluster --name xds-advanced
```

## Customization Ideas

### Add Rate Limiting

Integrate with external rate limiting service:

```yaml
- name: envoy.filters.http.ratelimit
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit
    domain: my_domain
    rate_limit_service:
      grpc_service:
        envoy_grpc:
          cluster_name: rate_limit_cluster
```

### Add Authentication

Add JWT authentication:

```yaml
- name: envoy.filters.http.jwt_authn
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.jwt_authn.v3.JwtAuthentication
```

## Next Steps

- [HTTPS/TLS](../https-tls/) - Add TLS termination
- [gRPC](../grpc/) - gRPC routing
- [Multi-Environment](../multi-environment/) - Production/staging configs
