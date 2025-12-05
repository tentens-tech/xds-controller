## RDS (Route Discovery Service)

The Route Discovery Service (RDS) is a component of the control-plane xDS service for Envoy that enables dynamic discovery of routing configuration for Envoy proxies. It allows you to configure and manage the routes that determine how traffic is routed to different backend services.

[Back to Main](../../README.md)

## Features

- Dynamic route configuration
- HTTP/HTTPS routing
- QUIC (HTTP/3) support
- Virtual host management
- Filter chain matching
- TLS certificate integration
- HTTP filter configuration

## Core Concepts

### Virtual Hosts

Virtual hosts define domain-specific routing rules. Each virtual host can handle multiple domains and contains its own set of routes.

### Filter Chains

Filter chains allow you to match specific traffic patterns and apply different routing rules based on criteria like server names, transport protocol, or source ports.

### HTTP Filters

HTTP filters process HTTP requests and responses, enabling features like compression, CORS, authentication, and more.

### Example Configurations

[RDS Example config](../../config/samples/rds_v1alpha1_route.yaml)

### Basic Route Configuration

The following is an example of a route configuration for the RDS:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: example-domain-route
spec:
  listener_refs:
    - https
  tlssecret_ref: example-cert
  filter_chain_match:
    server_names:
    - api.example.com
  ...
```

## Advanced Configurations

### HTTPS with QUIC Support

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: my-domain-route
  annotations:
    nodes: "01,02"
    clusters: production,staging
spec:
  listener_refs:
    - https
    - quic  # Enable both HTTPS and QUIC
  tlssecret_ref: my-domain-cert
  filter_chain_match:
    server_names:
      - mydomain.com
      - www.mydomain.com
  stat_prefix: http_mydomain
  generate_request_id: true
  codec_type: AUTO
  route_config:
    virtual_hosts:
      - name: local_service
        domains:
          - mydomain.com
          - www.mydomain.com
        routes:
          - match:
              prefix: /
            route:
              cluster: backend-service
```

### HTTP Filters Configuration

```yaml
spec:
  http_filters:
    - name: envoy.filters.http.cors
      typed_config:
        '@type': type.googleapis.com/envoy.extensions.filters.http.cors.v3.Cors
    - name: envoy.filters.http.compressor
      typed_config:
        '@type': type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
        response_direction_config:
          common_config:
            min_content_length: 1024
        compressor_library:
          name: text_optimized
          typed_config:
            '@type': type.googleapis.com/envoy.extensions.compression.brotli.compressor.v3.Brotli
            quality: 4
```

### Route with CORS, Compression & Access Logging

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: api-route
spec:
  listener_refs:
    - http
  stat_prefix: api
  use_remote_address: true
  generate_request_id: true
  codec_type: AUTO
  common_http_protocol_options:
    idle_timeout: 900s
  route_config:
    name: api_config
    virtual_hosts:
      - name: api_host
        domains: ["*"]
        cors:
          allow_credentials: true
          allow_origin_string_match:
            - safe_regex:
                regex: ".*"
          allow_methods: GET,POST,PUT,DELETE,OPTIONS,HEAD
          allow_headers: Authorization,Content-Type,X-Requested-With
          max_age: 24h
        response_headers_to_add:
          - header:
              key: x-api-version
              value: "v1"
        routes:
          - match:
              prefix: /api/
            request_headers_to_add:
              - header:
                  key: x-route
                  value: api
            route:
              prefix_rewrite: /
              timeout: 15s
              cluster: api-backend
              retry_policy:
                retry_host_predicate:
                  - name: envoy.retry_host_predicates.previous_hosts
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.retry.host.previous_hosts.v3.PreviousHostsPredicate
                host_selection_retry_max_attempts: 2
                retriable_status_codes: [503]
                retry_on: connect-failure,cancelled,retriable-status-codes
                num_retries: 3
          - match:
              prefix: /health
            route:
              timeout: 5s
              cluster: simple-cluster
          - match:
              prefix: /old/
            redirect:
              path_redirect: /new/
              response_code: MOVED_PERMANENTLY
          - match:
              prefix: /
            route:
              timeout: 15s
              cluster: simple-cluster
  access_log:
    - name: envoy.access_loggers.file
      filter:
        status_code_filter:
          comparison:
            op: GE
            value:
              default_value: 400
              runtime_key: access_log.error.status
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
        path: /dev/stdout
        log_format:
          json_format:
            time: "%START_TIME%"
            method: "%REQ(:METHOD)%"
            path: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"
            code: "%RESPONSE_CODE%"
            duration: "%DURATION%"
            upstream: "%UPSTREAM_HOST%"
            cluster: "%UPSTREAM_CLUSTER%"
            request_id: "%REQ(X-REQUEST-ID)%"
  http_filters:
    - name: envoy.filters.http.cors
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.cors.v3.Cors
    - name: envoy.filters.http.compressor
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
        response_direction_config:
          common_config:
            min_content_length: 100
        compressor_library:
          name: text_optimized
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.compression.brotli.compressor.v3.Brotli
            quality: 4
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

### Route with Lua Scripting

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: lua-route
spec:
  listener_refs:
    - http
  stat_prefix: lua
  route_config:
    virtual_hosts:
      - name: default
        domains: ["*"]
        routes:
          - match:
              prefix: /
            route:
              cluster: simple-cluster
  http_filters:
    - name: envoy.filters.http.lua
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
        inline_code: |
          function envoy_on_request(request_handle)
            request_handle:headers():add("x-lua-processed", "true")
          end
          function envoy_on_response(response_handle)
            response_handle:headers():add("x-lua-response", "processed")
          end
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

### Route with gRPC Support

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: grpc-route
spec:
  listener_refs:
    - http
  stat_prefix: grpc
  route_config:
    virtual_hosts:
      - name: grpc_host
        domains: ["*"]
        routes:
          - match:
              prefix: /grpc/
              headers:
                - name: content-type
                  prefix_match: "application/grpc"
            route:
              prefix_rewrite: /
              timeout: 30s
              cluster: grpc-backend
          - match:
              prefix: /
            route:
              cluster: simple-cluster
  http_filters:
    - name: envoy.grpc_web
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.grpc_web.v3.GrpcWeb
    - name: envoy.filters.http.grpc_stats
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.grpc_stats.v3.FilterConfig
        stats_for_all_methods: false
        enable_upstream_stats: true
        emit_filter_state: true
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

### HTTPS Route with Multiple Listeners (HTTPS + QUIC)

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: https-route
spec:
  listener_refs:
    - https   # HTTPS on port 443
    - quic    # QUIC/HTTP3 on port 443 UDP
  tlssecret_ref: wildcard-cert
  filter_chain_match:
    server_names:
      - secure.example.com
  stat_prefix: https
  use_remote_address: true
  codec_type: AUTO
  route_config:
    name: https_config
    virtual_hosts:
      - name: secure_host
        domains: ["*"]
        response_headers_to_add:
          - header:
              key: strict-transport-security
              value: "max-age=31536000; includeSubDomains"
          - header:
              key: alt-svc
              value: "h3=\":443\"; ma=86400"
        routes:
          - match:
              prefix: /
            route:
              timeout: 15s
              cluster: simple-cluster
  http_filters:
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

## Configuration Parameters

### Required Parameters

- `metadata.name`: Specifies the name of the route
- `spec.listener_refs`: Array of listener names to attach to (e.g., `["https", "quic"]`)
- `spec.filter_chain_match`: Defines matching criteria for the route

### Optional Parameters

- `spec.tlssecret_ref`: Specifies the TLS certificate (references a TLSSecret CR name)
- `spec.codec_type`: Specifies the codec type (use uppercase)
- `spec.stat_prefix`: Statistics prefix
- `spec.access_log`: Access logging configuration
- `spec.use_remote_address`: Remote address usage in forwarding
- `spec.http_filters`: HTTP filter configurations
- `spec.common_http_protocol_options`: HTTP protocol options
- `spec.stream_error_on_invalid_http_message`: Error handling for invalid HTTP

## Annotations

### Optional Annotations (for targeting specific Envoy instances)

- `clusters`: Comma-separated list of Envoy clusters (e.g., "production,staging")
- `nodes`: Comma-separated list of Envoy node IDs (e.g., "01,02")

> **Note:** If no annotations are specified, the route is sent to the default node (`global/global`).

## Parameter Guidelines

- Parameters like `codec_type` should be named in uppercase, as they represent types.
- Parameters that have a boolean type, such as `use_remote_address`, `stream_error_on_invalid_http_message`, `generate_request_id` and any other boolean type should ***NOT*** be specified as boolean strings. Instead, use the actual boolean value when configuring these parameters. For example, use `true` or `false`, without quotes, to represent `boolean true` or `boolean false`, respectively.

## Best Practices

1. **Route Organization**
   - Use descriptive route names
   - Group related routes by domain
   - Keep virtual host configurations clean

2. **Security**
   - Always use TLS for HTTPS routes
   - Configure CORS appropriately
   - Implement proper access logging

3. **Performance**
   - Enable compression for appropriate content types
   - Configure proper buffer sizes
   - Use appropriate codec types

## Integration

### With LDS

Routes are automatically integrated with listeners based on `spec.listener_refs`. Make sure:

- Listener names in `listener_refs` match exactly with Listener resource names
- TLS is configured for HTTPS routes via `tlssecret_ref`
- Include "quic" in `listener_refs` if using HTTP/3

### With SDS

When using TLS:

- Set `spec.tlssecret_ref` to the TLSSecret CR name
- Ensure the TLSSecret exists
- Match domain names with certificates

## Troubleshooting

Common issues and solutions:

1. **Routing Issues**
   - Verify virtual host domain matches
   - Check filter chain matching criteria
   - Validate cluster references

2. **TLS Problems**
   - Confirm tlssecret_ref matches TLSSecret CR name
   - Verify certificate validity
   - Check server name matching

3. **HTTP Filter Issues**
   - Validate filter configurations
   - Check filter order
   - Verify typed configs

For more details about the RDS configuration, refer to the [official Envoy RDS API documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto.html).
