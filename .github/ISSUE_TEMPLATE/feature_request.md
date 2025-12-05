---
name: Feature Request
about: Suggest an idea for the Envoy xDS Controller
title: '[FEATURE] '
labels: enhancement
assignees: ''
---

## Feature Description

A clear and concise description of the feature you'd like to see added.

## Problem / Use Case

Is your feature request related to a problem? Please describe.
A clear and concise description of what the problem is. Ex. I'm always frustrated when [...]

## Proposed Solution

A clear and concise description of what you want to happen.

## Example Configuration

If applicable, show what the feature would look like in a CRD:

```yaml
apiVersion: envoyxds.io/v1alpha1
kind: Listener  # or Route, Cluster, etc.
metadata:
  name: example
spec:
  # Show proposed configuration
```

## Alternative Solutions

A clear and concise description of any alternative solutions or features you've considered.

## Implementation Considerations

If you have ideas about how this could be implemented, please share them here:

- [ ] Requires new CRD fields
- [ ] Requires new CRD type
- [ ] Backwards compatible
- [ ] Breaking change
- [ ] Requires Envoy version >= X.X

## xDS Protocol Reference

If applicable, link to relevant Envoy/xDS documentation:

- [Envoy Documentation](https://www.envoyproxy.io/docs)
- [xDS Protocol](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol)

## Additional Context

Add any other context or screenshots about the feature request here.

## Priority

How important is this feature to you?

- [ ] Nice to have
- [ ] Would be helpful
- [ ] Important for my use case
- [ ] Critical / Blocking
