---
name: Bug Report
about: Create a report to help us improve the Envoy xDS Controller
title: '[BUG] '
labels: bug
assignees: ''
---

## Bug Description

A clear and concise description of what the bug is.

## Steps to Reproduce

1. Apply resource '...'
2. Configure Envoy with '...'
3. Observe '...'
4. See error

## Expected Behavior

A clear and concise description of what you expected to happen.

## Actual Behavior

A clear and concise description of what actually happened.

## Environment Information

- **Kubernetes Version**: [e.g., 1.28.0]
- **xDS Controller Version**: [e.g., v1.0.0]
- **Envoy Version**: [e.g., 1.28.0]
- **Deployment Method**: [e.g., Helm, kubectl, Docker]
- **Cloud Provider**: [e.g., AWS, GCP, Azure, On-premise]

## Resource Configurations

If applicable, include your Custom Resource definitions (sanitize sensitive data):

```yaml
# Paste relevant CRDs here (Listener, Route, Cluster, Endpoint, TLSSecret)
```

## Controller Logs

If applicable, include relevant controller logs:

```text
Paste controller logs here
```

## Envoy Logs

If applicable, include relevant Envoy proxy logs:

```text
Paste Envoy logs here
```

## Additional Context

Add any other context about the problem here, such as:

- Network topology
- TLS/certificate related issues
- DNS provider (for ACME challenges)
- Vault integration details (if applicable)

## Checklist

- [ ] I have searched existing issues to ensure this is not a duplicate
- [ ] I have included all relevant configuration and logs
- [ ] I have sanitized any sensitive information
