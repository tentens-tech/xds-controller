# Security Policy

## Supported Versions

We release patches for security vulnerabilities for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

The Envoy xDS Controller team takes security bugs seriously. We appreciate your efforts to responsibly disclose your findings, and will make every effort to acknowledge your contributions.

### How to Report

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report security vulnerabilities by emailing:

**[yurii.zheliezko@tentens.tech](mailto:yurii.zheliezko@tentens.tech)**

Please include the following information in your report:

- **Type of issue** (e.g., buffer overflow, SQL injection, cross-site scripting, etc.)
- **Full paths of source file(s)** related to the manifestation of the issue
- **Location of the affected source code** (tag/branch/commit or direct URL)
- **Any special configuration required** to reproduce the issue
- **Step-by-step instructions** to reproduce the issue
- **Proof-of-concept or exploit code** (if possible)
- **Impact of the issue**, including how an attacker might exploit the issue

### What to Expect

- **Acknowledgment**: We will acknowledge receipt of your vulnerability report within 48 hours.
- **Communication**: We will keep you informed about our progress throughout the process.
- **Resolution**: We aim to resolve critical issues within 90 days of the initial report.
- **Credit**: If you would like, we will credit you in any public disclosure of the vulnerability.

## Security Considerations

### TLS/Certificate Management

The xDS Controller handles TLS certificates for Envoy proxies. Key security considerations:

- **Private Key Storage**: Private keys can be stored in HashiCorp Vault or locally. We recommend using Vault for production deployments.
- **Let's Encrypt Integration**: The controller supports automatic certificate provisioning via Let's Encrypt (ACME protocol).
- **Certificate Rotation**: Certificates are automatically renewed before expiration.

### Kubernetes RBAC

The controller requires specific RBAC permissions to function:

- Read/write access to Custom Resources (Listeners, Routes, Clusters, Endpoints, TLSSecrets)
- Read access to Kubernetes Secrets (for TLS secrets)
- Leader election resources

Review the RBAC configuration in `config/rbac/` to ensure it meets your security requirements.

### Network Security

- **gRPC Server**: The xDS gRPC server listens on port 18000 by default. Consider using network policies to restrict access.
- **Metrics Endpoint**: Prometheus metrics are exposed on port 8080. Ensure this is not publicly accessible.

### Secrets Management

When using the controller with sensitive data:

1. **Vault Integration**: Use HashiCorp Vault for storing TLS private keys and other secrets.
2. **Kubernetes Secrets**: If using Kubernetes secrets, ensure proper RBAC and encryption at rest.
3. **Environment Variables**: Avoid passing sensitive data via environment variables in production.

### Container Security

- The controller runs as a non-root user (UID 65532)
- The container uses a minimal base image (Alpine or distroless)
- No unnecessary capabilities are required

## Security Best Practices

### For Operators

1. **Keep Updated**: Regularly update to the latest version of the controller
2. **Network Policies**: Implement Kubernetes network policies to restrict controller communication
3. **RBAC**: Follow the principle of least privilege when configuring RBAC
4. **Audit Logging**: Enable Kubernetes audit logging to track access to sensitive resources
5. **Secrets Encryption**: Enable encryption at rest for Kubernetes secrets

### For Contributors

1. **Dependency Scanning**: Dependabot is configured to scan for vulnerable dependencies
2. **Code Review**: All changes require code review before merging
3. **Security Testing**: Include security-focused tests for new features
4. **Static Analysis**: golangci-lint and gosec are run as part of CI

## Known Security Limitations

- The controller trusts all Envoy clients connecting to the xDS server. Implement network-level controls to restrict access.
- ACME DNS-01 challenges require DNS provider credentials, which should be securely managed.

## Security Updates

Security updates will be released as patch versions (e.g., 1.0.1) and announced via:

- GitHub Security Advisories
- Release notes

## Attribution

We would like to thank the following individuals for responsibly disclosing security issues:

*None yet - be the first!*
