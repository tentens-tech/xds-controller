# LDS (Listener Discovery Service)

The Listener Discovery Service (LDS) is a component of the control-plane xDS service for Envoy that enables dynamic discovery and configuration of listeners for Envoy proxies. It allows you to define and manage how Envoy proxies handle incoming network traffic.

[Back to Main](../../README.md)

## Features

- HTTP/HTTPS listener support
- QUIC (HTTP/3) support
- TCP proxy capabilities
- TLS inspector integration
- Automatic filter chain configuration
- Dynamic secret management through SDS

### Example Configurations

[LDS Example config](../../config/samples/lds_v1alpha1_listener.yaml)

### Listener Configuration Example

The following are examples of listener configurations for the LDS:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: http
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 80
```

```yaml
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
    - name: "envoy.filters.listener.tls_inspector"
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.listener.tls_inspector.v3.TlsInspector
```

In these examples, certain parameters are specified:

- `metadata.name`: Specifies the name of the listener. For example, `http` and `https`.
- `spec.address`: Specifies the address where the listener will bind, such as `0.0.0.0` on port `80` for HTTP and port `443` for HTTPS.
- `spec.listener_filters`: Specifies the listener filters to apply to the listener. In the second example, the `tls_inspector` filter is added.

For more detailed information on configuring listeners in the LDS, please refer to the [official Envoy LDS API documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener.proto).

## Advanced Configurations

### QUIC Support

To enable QUIC (HTTP/3) for a listener:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  annotations:
    clusters: production,staging
    nodes: 01,02
  name: quic
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 443
      protocol: UDP
  udp_listener_config:
    downstream_socket_config:
      prefer_gro: true
    quic_options: {}
```

### TCP Proxy

For TCP proxy configurations:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  annotations:
    clusters: production
    nodes: 01,02
  name: tcp-proxy
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 5672
  filter_chains:
    - filters:
        - name: envoy.filters.network.tcp_proxy
          typed_config:
            '@type': type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
            cluster: upstream-cluster
            stat_prefix: tcp_stats
```

### Multiple TCP Services on Different Ports

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: redis-proxy
spec:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 6379
  filter_chains:
    - filters:
        - name: envoy.filters.network.tcp_proxy
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
            stat_prefix: redis_stats
            cluster: redis-cluster
---
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata:
  name: postgres-proxy
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
            stat_prefix: postgres_stats
            cluster: postgres-cluster
```

## RDS and Automatic Filter Chain Configuration

When using the Route Discovery Service (RDS), each Route adds one filter chain to every listener named in its `spec.listener_refs` list, on every node the Route targets. Listener names in `listener_refs` must match the `metadata.name` of the Listener resources.

The controller rebuilds a listener whenever RDS adds, changes, or removes a Route on it, including when a listener is dropped from a Route's `listener_refs`.

Filter chains from Routes are added after any `filter_chains` written in the Listener spec, oldest Route first (by `creationTimestamp`, then name). This order is stable across controller restarts.

RDS rejects Routes whose filter chains conflict with each other or with the static `filter_chains` in the Listener spec, before they reach the listener, and reports it on the Route (see [Filter Chain Matching and Conflicts](../rds/README.md#filter-chain-matching-and-conflicts)).

LDS still checks every chain it adds, as a last guard. A Route chain that Envoy would reject next to a chain already on the listener, or one that fails to convert, is skipped and the rest of the listener keeps updating. A skip shows up in the controller log and in `xds_config_error_count`, not on the Route's status. The Route's unused route configuration is left out of the snapshot.

Static `filter_chains` that Envoy cannot tell apart from each other, or that have an invalid match (for example a malformed IP address), fail the listener build. The Listener's status reports the error and the last accepted listener keeps serving.

## Automatic SDS Secret Configuration

In the LDS, the Secret Discovery Service (SDS) secrets can be automatically applied to the `filter_chain.transport_socket` when you reference TLSSecret CRs from the Route spec using the name of the desired TLSSecret CR.

### `tlssecret_ref` (single secret)

By setting `spec.tlssecret_ref` to the name of a TLSSecret CR, the control plane attaches that SDS secret to the route’s filter chain transport socket on the listener.

### `tlssecret_refs` (multiple secrets)

Use `spec.tlssecret_refs` when the same route’s filter chain should load more than one TLS certificate via SDS—for example, multiple `TlsCertificateSdsSecretConfigs` for SNI or additional keypairs.

By setting `spec.tlssecret_ref` or `spec.tlssecret_refs` in the Route configuration, the xDS control-plane ensures automatic application of the corresponding SDS secret to the `filter_chain.transport_socket` of the desired listener. This allows for secure communication between Envoy proxies and backend services by automatically injecting the required secrets.

- You can set **only** `tlssecret_refs`, **only** `tlssecret_ref`, or **both**.
- When both are set, `tlssecret_ref` is applied **first**, then each entry in `tlssecret_refs` in list order.
- Duplicate secret names (between the two fields or inside `tlssecret_refs`) are removed. Empty strings are skipped; surrounding whitespace on names is trimmed.

This automatic configuration simplifies the management of secrets within the LDS and eliminates manual intervention when associating secrets with the transport socket of listeners. Each name must match the `metadata.name` of an existing TLSSecret CR.

This automatic configuration simplifies secret management on the listener and avoids hand-editing transport sockets for TLS.

Please note that the `tlssecret_ref` and `tlssecret_refs` fields in the Route spec should match the name of the desired TLSSecret CR.

## Annotations and Parameters

### Optional Annotations (for targeting specific Envoy instances)

- `clusters`: Comma-separated list of Envoy clusters (e.g., "production,staging")
- `nodes`: Comma-separated list of Envoy node IDs (e.g., "01,02")

> **Note:** If no annotations are specified, the listener is sent to the default node (`global/global`).

### Parameter Guidelines

- Parameters like `*_type` should be named in uppercase, as they represent types.
- Parameters that have a boolean type should ***NOT*** be specified as boolean strings. Instead, use the actual boolean value when configuring these parameters. For example, use `true` or `false`, without quotes, to represent `boolean true` or `boolean false`, respectively.

## Best Practices

1. **Listener Naming**
   - Use descriptive names that indicate the purpose (e.g., "http", "https", "quic")
   - Keep names consistent across your configuration

2. **Port Configuration**
   - Use standard ports when possible (80 for HTTP, 443 for HTTPS/QUIC)
   - Document any non-standard port usage

3. **Security**
   - Always use TLS inspector for HTTPS listeners
   - Configure appropriate security filters
   - Follow least privilege principle in annotations

## Troubleshooting

Common issues and solutions:

1. **Listener Binding Failures**
   - Verify port availability
   - Check address binding permissions
   - Ensure no port conflicts

2. **TLS Issues**
   - Verify TLS inspector configuration
   - Check SDS secret availability
   - Validate certificate configurations

3. **Filter Chain Problems**
   - Verify route configurations
   - Check listener name matches in routes
   - Validate filter configurations
   - A Route missing from the listener with "filter chain conflict" in its status lost to an older Route; see [How a conflict is resolved](../rds/README.md#how-a-conflict-is-resolved)
