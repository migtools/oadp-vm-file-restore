# AGENTS.md — AI Agent Instructions for migtools/oadp-vm-file-restore

## Project Overview
OADP VM File Restore provides a Kubernetes controller for discovering and serving files from VM backups created by Velero/OADP. It enables users to browse and restore individual files from virtual machine backup volumes without needing to restore the entire VM. Built with the Operator SDK using controller-runtime.

- **Primary Language**: Go
- **Module**: `github.com/migtools/oadp-vm-file-restore`
- **Default Branch**: `oadp-dev`

## Build Instructions
```bash
# Build the controller binary (includes manifests, codegen, fmt, vet)
make build

# Build Docker image
make docker-build

# Push Docker image
make docker-push

# Build cross-platform image
make docker-buildx

# Run the controller locally
make run

# Generate consolidated installer YAML
make build-installer
```

## Test Instructions
```bash
# Run all tests (includes manifests, codegen, fmt, vet, envtest)
make test

# Run specific tests
go test ./internal/controller/... -run TestName

# Format code
make fmt

# Vet code
make vet
```

## Linting
```bash
# Run golangci-lint
make lint

# Run golangci-lint with auto-fix
make lint-fix

# Verify linter config
make lint-config
```

Configuration: `.golangci.yml`

## Code Generation
```bash
# Generate CRD manifests
make manifests

# Generate DeepCopy methods
make generate
```

## Code Conventions
- Operator SDK / controller-runtime patterns
- API types in `api/` with version directories
- Controllers in `internal/controller/`
- CRD and RBAC manifests in `config/`
- E2E tests in `test/`
- Container definitions in `containers/`
- Documentation in `docs/`
- Use kubebuilder markers for RBAC and CRD generation

## Project Structure
```
api/            - API types (VMFileRestore CRDs)
cmd/            - Controller entry point
config/         - Kubernetes manifests
  crd/          - CRD definitions
  rbac/         - RBAC rules
  manager/      - Deployment manifests
  samples/      - Example CR instances
containers/     - Container image definitions
docs/           - Documentation
hack/           - Development and CI scripts
internal/       - Private packages
  controller/   - Reconciler implementations
test/           - E2E and integration tests
test-manifests/ - Test fixture manifests
```

## CI/CD
- GitHub Actions workflows in `.github/workflows/`:
  - `lint.yml` — Linting checks
  - `test.yml` — Unit tests
  - `test-e2e.yml` — E2E tests
- Reproduce CI locally:
  ```bash
  make build
  make lint
  make test
  ```

## Common Tasks

### Adding a new API type
1. Define the type in `api/`
2. Run `make generate` for DeepCopy methods
3. Run `make manifests` for CRD YAML
4. Create reconciler in `internal/controller/`
5. Add tests

### Working with container images
- Container definitions in `containers/`
- Main Dockerfile at project root
