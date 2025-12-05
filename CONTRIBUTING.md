# Contributing to Envoy xDS Controller

First off, thank you for considering contributing to the Envoy xDS Controller! It's people like you that make this project better for everyone.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Testing](#testing)
- [Submitting Changes](#submitting-changes)
- [Style Guidelines](#style-guidelines)
- [Community](#community)

## Code of Conduct

This project and everyone participating in it is governed by our [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

- Go 1.24+
- Docker
- kubectl
- A Kubernetes cluster (local: [KIND](https://sigs.k8s.io/kind), [minikube](https://minikube.sigs.k8s.io/), or [k3d](https://k3d.io/))
- make

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:

   ```bash
   git clone https://github.com/YOUR_USERNAME/xds.git
   cd xds
   ```

3. Add the upstream repository:

   ```bash
   git remote add upstream https://github.com/tentens-tech/xds-controller.git
   ```

## Development Setup

### Install Dependencies

```bash
# Download Go modules
go mod download

# Install development tools
make controller-gen
make kustomize
make envtest
```

### Build the Project

```bash
# Build the binary
make build

# Build Docker image
make docker-build IMG=local/xds:dev
```

### Run Locally

```bash
# Install CRDs to your cluster
make install

# Run the controller locally
make run
```

## Making Changes

### Branch Naming

Use descriptive branch names:

- `feature/add-quic-support`
- `fix/certificate-renewal-issue`
- `docs/update-installation-guide`
- `refactor/cleanup-controller-logic`

### Commit Messages

Follow the conventional commits specification:

```text
type(scope): short description

Longer description if needed.

Fixes #123
```

Types:

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Working with CRDs

If you modify API types in `apis/v1alpha1/`:

```bash
# Regenerate DeepCopy methods and CRDs
make generate
make manifests
```

### Adding New xDS Types

The project includes type generators for xDS resources:

```bash
# Generate all xDS types
make generate-types

# Or generate individually
make generate-lds
make generate-cds
make generate-rds
make generate-eds
```

## Testing

### Run All Tests

```bash
make test
```

### Run Specific Tests

```bash
# Run tests for a specific package
go test -v ./controllers/lds/...

# Run with coverage
make cover
```

### Integration Testing

For integration tests with a real Kubernetes cluster:

```bash
# Create a test cluster with KIND
kind create cluster --name xds-test

# Install CRDs
make install

# Apply sample resources
kubectl apply -f config/samples/
```

## Submitting Changes

### Before Submitting

1. Ensure all tests pass:

   ```bash
   make test
   ```

2. Run linting:

   ```bash
   make fmt
   make vet
   ```

3. Ensure manifests are up to date:

   ```bash
   make manifests
   make generate
   ```

4. Check for uncommitted changes:

   ```bash
   git status
   ```

### Pull Request Process

1. Update documentation if needed
2. Add tests for new functionality
3. Ensure the CI pipeline passes
4. Request review from maintainers
5. Address review feedback
6. Once approved, a maintainer will merge your PR

### Pull Request Checklist

- [ ] Tests pass locally
- [ ] Code follows style guidelines
- [ ] Documentation updated (if applicable)
- [ ] CHANGELOG updated (for significant changes)
- [ ] Commits are signed (if required)

## Style Guidelines

### Go Code

- Follow the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `gofmt` for formatting
- Run `go vet` before committing
- Keep functions focused and small
- Add comments for exported functions and types

### Documentation

- Use clear, concise language
- Include code examples where helpful
- Keep README sections up to date
- Add inline comments for complex logic

### YAML/Kubernetes Resources

- Use 2-space indentation
- Add comments explaining non-obvious configurations
- Follow Kubernetes naming conventions

## Project Structure

```text
xds/
├── apis/v1alpha1/      # Kubernetes API types (CRDs)
├── cmd/xds/            # Main application entrypoint
├── config/             # Kubernetes manifests
│   ├── crd/            # CRD definitions
│   ├── rbac/           # RBAC resources
│   └── samples/        # Example resources
├── controllers/        # Kubernetes controllers
│   ├── cds/            # Cluster Discovery Service
│   ├── eds/            # Endpoint Discovery Service
│   ├── lds/            # Listener Discovery Service
│   ├── rds/            # Route Discovery Service
│   └── sds/            # Secret Discovery Service
├── pkg/                # Shared packages
│   ├── app/            # Application configuration
│   ├── serv/           # gRPC server
│   └── xds/            # xDS implementation
└── tools/              # Code generation tools
```

## Community

### Getting Help

- Open a [GitHub Issue](https://github.com/tentens-tech/xds-controller/issues)
- Check existing issues and discussions
- Review the [documentation](README.md)

### Reporting Bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md) and include:

- Steps to reproduce
- Expected vs actual behavior
- Environment details
- Relevant logs

### Requesting Features

Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md) and describe:

- The problem you're trying to solve
- Your proposed solution
- Alternative approaches considered

## License

By contributing to this project, you agree that your contributions will be licensed under the Apache License 2.0.

---

Thank you for contributing! 🎉
