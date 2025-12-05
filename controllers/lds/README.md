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

When using the Route Discovery Service (RDS), the filter chains will be automatically added to the proper listener based on the `listener_ref` field in the Route spec. Routes specify which listener(s) they attach to using `spec.listener_ref` (comma-separated for multiple listeners).

By specifying the listener name in the RDS configuration, the xDS control-plane will ensure that the corresponding filter chains are added to the appropriate listener in the Listener Discovery Service (LDS). This dynamic configuration allows for flexible and scalable management of filter chains based on the specific needs of each listener.

Please note that the listener name specified in the RDS configuration must match the listener name defined in the LDS configuration. This ensures that the filter chains are correctly associated with the desired listener.

This automatic filter chain configuration simplifies the management of filter chains and makes it easier to maintain a clean and organized setup for handling incoming network traffic.

## Automatic SDS Secret Configuration

In the LDS, the Secret Discovery Service (SDS) secrets can be automatically applied to the `filter_chain.transport_socket` when you set the `tlssecret_ref` field in the Route spec using the name of the desired TLSSecret CR.

By setting `spec.tlssecret_ref` in the Route configuration, the xDS control-plane ensures automatic application of the corresponding SDS secret to the `filter_chain.transport_socket` of the desired listener. This allows for secure communication between Envoy proxies and backend services by automatically injecting the required secrets.

This automatic configuration simplifies the management of secrets within the LDS and eliminates manual intervention when associating secrets with the transport socket of listeners.

Please note that the `tlssecret_ref` field in the Route spec should match the name of the desired TLSSecret CR.

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
