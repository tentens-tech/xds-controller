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

Filter chains allow you to match specific traffic patterns and apply different routing rules based on criteria like server names, transport protocol, or source ports. See [Filter Chain Matching and Conflicts](#filter-chain-matching-and-conflicts) for which Routes can share a listener.

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

## Filter Chain Matching and Conflicts

Every Route becomes one filter chain on each listener in `listener_refs`, on each node it targets. Envoy picks a chain by walking the match criteria in a fixed order and keeping the most specific match at each step:

`destination_port` → `prefix_ranges` → `server_names` → `transport_protocol` → `application_protocols` → `direct_source_prefix_ranges` → `source_type` → `source_prefix_ranges` → `source_ports`

An empty criterion counts as its own value, and chains that set it win over chains that leave it empty. Envoy rejects the whole listener when two chains land on the same value at every step. The controller runs the same check before it sends the config, so a bad pair of Routes never reaches Envoy. The rules below are checked against Envoy 1.31 and 1.36 in CI.

### When two Routes conflict

Two Routes conflict only when all of these are true:

1. They target at least one common node (cluster × node from the `clusters` and `nodes` annotations).
2. They share at least one listener in `listener_refs`.
3. Their `filter_chain_match` share a value on every criterion above. A Route without `filter_chain_match` matches everything, so two such Routes on one listener conflict.

A Route also conflicts with the `filter_chains` written in a Listener's own spec, on the nodes where that Listener exists. Those chains always win.

On top of Envoy's rule, the controller treats two more `server_names` pairs as a conflict when the Routes' virtual hosts share a domain:

- a wildcard that covers another Route's exact name one label down (`*.example.com` and `api.example.com`);
- names that differ only by a trailing dot (`example.com.` and `example.com`).

Envoy would accept these pairs, but one Route would silently lose traffic to the other.

CIDRs are shortened to `address/prefix_len` in this table.

| Route A `filter_chain_match` | Route B `filter_chain_match` | Result |
| ---------------------------- | ---------------------------- | ------ |
| `server_names: [shop.example.com]` | `server_names: [shop.example.com]` | conflict |
| `server_names: [shop.example.com]`, other cluster | `server_names: [shop.example.com]` | no conflict |
| `server_names: [shop.example.com]`, other listener | `server_names: [shop.example.com]` | no conflict |
| none | none | conflict |
| `source_prefix_ranges: [35.191.0.0/16]` | `source_prefix_ranges: [35.191.0.0/16]` | conflict |
| `source_prefix_ranges: [35.191.0.0/16]` | `source_prefix_ranges: [35.191.0.0/17]` | no conflict (longest prefix wins) |
| `source_prefix_ranges: [X]`, `application_protocols: [h2, http/1.1]` | `source_prefix_ranges: [X]` | no conflict |
| `application_protocols: [h2, http/1.1]` | `application_protocols: [h2]` | conflict |
| `destination_port: 443` | none | no conflict |
| `server_names: ["*.example.com"]` | `server_names: [api.example.com]` | conflict only if virtual host domains overlap |
| `server_names: [".example.com"]` | `server_names: ["*.example.com"]` | conflict (Envoy files both under `.example.com`) |
| `source_prefix_ranges: [0.0.0.0/0]` | none | conflict (see below) |

Values are compared the way Envoy reads them:

- An unset `prefix_len` means `0`, so `{address_prefix: 10.1.2.3}` matches every IPv4 address. Always set `prefix_len`.
- Addresses are masked to their prefix: `35.191.7.7/16` equals `35.191.0.0/16`. A `prefix_len` longer than the address (up to 128) is clamped.
- An explicit `0.0.0.0/0` or `::/0` conflicts with a Route that leaves the same range unset. Envoy accepts the pair, but which chain gets the traffic depends on declaration order and changes between Envoy versions.
- An IPv4-mapped IPv6 address (`::ffff:1.2.3.4`) is not the same as `1.2.3.4`.
- `server_names` compare case-insensitively (ASCII letters only). An empty string in `server_names` or `application_protocols` is the same as leaving the field unset.
- `source_type: ANY` is the same as leaving `source_type` unset. Write enum values in upper case (`EXTERNAL`, not `external`).

A Route is rejected on its own, before any comparison, when its `filter_chain_match` has:

- a malformed IP address, a `prefix_len` above 128, or a port outside 1–65535;
- a partial wildcard in `server_names`, such as `*`, `*example.com` or `a.*.com`;
- the same value twice in one list, after the normalization above (`[a.com, A.com]`, `[10.0.0.0/8, 10.9.0.0/8]`);
- `address_suffix` or `suffix_len`, which Envoy does not implement.

### How a conflict is resolved

The older Route wins: earlier `metadata.creationTimestamp`, then name in alphabetical order when both were created in the same second. The winner is the same whichever Route the controller sees first.

The losing Route is:

- removed from every node, not only the node where the conflict was found;
- marked `status.active: false`, with an `Error` condition and a `status.message` naming the winning Route;
- counted in `xds_config_error_count`.

When the winning Route or Listener is changed or deleted, the controller checks the losing Route again and serves it if the conflict is gone. You don't need to edit it.

When an edit makes a Route invalid, the controller reports the error on the Route and keeps serving its last accepted version on the same nodes as before.

A Route that is not served for another reason also says why in `status.message`: `listener_refs` is empty, or none of its listeners exists on the Route's nodes.

### Upgrading from earlier versions

Earlier versions compared Routes across all clusters and listeners, ignored `application_protocols`, `transport_protocol` and most other criteria, and never matched `source_prefix_ranges` that had `prefix_len` set. After upgrading:

- Routes that were rejected only because of a Route on another cluster, node or listener, or one with different `application_protocols` or `transport_protocol`, start being served.
- Pairs Envoy rejects or routes unpredictably, such as two Routes without `filter_chain_match` on one listener or identical `source_prefix_ranges`, now report a conflict on the newer Route instead of breaking the listener.

Before upgrading, look for Routes whose `status.message` mentions "filter chain match overlap" to see which ones will start serving, and run `route-audit` (below) to see which ones will be rejected.

### Example: health checks next to a CDN chain

Cloud load balancer health probes often connect over TLS with neither SNI nor ALPN, so no `server_names` chain matches them. A second chain for the probe source ranges without `application_protocols` catches them, while real clients from the same ranges keep using the ALPN chain:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: cdn-chain
spec:
  listener_refs: [https]
  tlssecret_ref: my-cert
  filter_chain_match:
    source_prefix_ranges:
      - {address_prefix: 35.191.0.0, prefix_len: 16}
      - {address_prefix: 130.211.0.0, prefix_len: 22}
    application_protocols: [h2, http/1.1]
  # ...
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata:
  name: lb-health-probe
spec:
  listener_refs: [https]  # TCP listener only; probes do not use QUIC
  tlssecret_ref: my-cert
  filter_chain_match:
    source_prefix_ranges:
      - {address_prefix: 35.191.0.0, prefix_len: 16}
      - {address_prefix: 130.211.0.0, prefix_len: 22}
  stat_prefix: lb-health-probe
  codec_type: AUTO
  route_config:
    name: lb_health_probe
    virtual_hosts:
      - name: lb_health_probe
        domains: ["*"]
        routes:
          - match: {prefix: /healthz}
            direct_response:
              status: 200
              body: {inline_string: "ok"}
  http_filters:
    - name: envoy.filters.http.router
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

Requests with SNI still go to their `server_names` chains, because Envoy checks `server_names` before source addresses.

### Checking Routes with route-audit

`route-audit` replays how the controller places Routes, oldest first, and prints each Route it would reject and why. It needs no cluster access, so it also works on manifests you haven't applied yet:

```sh
kubectl get routes.envoyxds.io,listeners.envoyxds.io -n xds-system -o yaml > live.yaml
go run ./cmd/route-audit -nodeID global -cluster global live.yaml new-route.yaml
```

```text
CONFLICT lb-health-probe evicts lb-health-probe-copy (duplicate) on production/01
3 routes, 1 problems
```

- Use the controller's namespace; the controller only sees Routes in its own namespace.
- Pass the controller's `--nodeID` and `--cluster` values, so Routes without annotations are placed the same way.
- Manifests without `metadata.creationTimestamp` count as newer than every Route already in the cluster.
- Include the Listeners, so Routes are placed only where their listeners exist and are checked against the Listeners' own `filter_chains`. Without Listeners, every listener is assumed to exist everywhere.
- Pass any number of files, each a `kubectl` List or multi-document YAML; with no files it reads stdin. It exits with status 1 when it reports a problem, and reads only `envoyxds.io` objects.

## Configuration Parameters

### Required Parameters

- `metadata.name`: Specifies the name of the route
- `spec.listener_refs`: Array of listener names to attach to (e.g., `["https", "quic"]`)

### Optional Parameters

- `spec.filter_chain_match`: Defines matching criteria for the route. Without it the route matches every connection on its listeners, so only one such route fits per listener
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

2. **Route inactive with "filter chain conflict"**
   - `status.message` names the older Route or the Listener that won; see [How a conflict is resolved](#how-a-conflict-is-resolved)
   - Change `filter_chain_match`, `listener_refs` or the `clusters`/`nodes` annotations of either one so they no longer collide
   - Run [route-audit](#checking-routes-with-route-audit) over all Routes and Listeners to find every conflict at once

3. **TLS Problems**
   - Confirm tlssecret_ref matches TLSSecret CR name
   - Verify certificate validity
   - Check server name matching

4. **HTTP Filter Issues**
   - Validate filter configurations
   - Check filter order
   - Verify typed configs

For more details about the RDS configuration, refer to the [official Envoy RDS API documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto.html).
