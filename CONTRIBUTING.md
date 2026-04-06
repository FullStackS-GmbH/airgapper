# Contributing to Universal Airgapper

Thank you for your interest in contributing to Universal Airgapper. This document covers everything you need to get started.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Architecture](#architecture)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Adding a New Transport Type](#adding-a-new-transport-type)
- [Adding a New Scanner](#adding-a-new-scanner)
- [Commit Conventions](#commit-conventions)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Code of Conduct](#code-of-conduct)

## Getting Started

### Prerequisites

- **Go 1.23+** -- the minimum supported Go version
- **direnv** -- automatically loads environment from `.envrc` ([direnv.net](https://direnv.net/))
- **Nix** (optional) -- provides a reproducible toolchain via `flake.nix` ([nixos.org](https://nixos.org/))
- **Docker** -- for building and testing container images
- **Git** -- obviously

### Fork and Clone

1. Fork the repository on GitHub.
2. Clone your fork locally:
   ```shell
   git clone https://github.com/YOUR-USERNAME/universal-airgapper.git
   cd universal-airgapper
   ```
3. Create a branch for your changes:
   ```shell
   git checkout -b task/your-feature-name
   ```

## Development Setup

If you use Nix + direnv, the toolchain is set up automatically:

```shell
direnv allow
```

Otherwise, ensure Go 1.23+ is installed and run:

```shell
go mod tidy
go build ./cmd/airgapper
```

Verify your setup:

```shell
go run ./cmd/airgapper -- version
go test ./...
```

## Project Structure

```
cmd/
  airgapper/
    main.go                   # Thin entrypoint -- wires deps, calls root command

internal/
  cli/                        # Cobra command definitions (root, sync, version)
    root.go                   # Root command, global persistent flags, viper binding
    sync.go                   # "sync" subcommand -- loads config, creates pipeline, runs engine
    version.go                # "version" subcommand -- prints build info

  config/                     # Configuration loading, validation, type definitions
    loader.go                 # Discover and merge *.airgapper.yaml files
    validate.go               # Schema validation, fail fast on errors
    types.go                  # Typed Go structs mirroring the YAML schema

  credentials/                # File-based credential store
    store.go                  # FileStore implementing domain.CredentialStore
    resolver.go               # Resolution by name reference or hostname match

  domain/                     # Core business types and interfaces (zero deps)
    types.go                  # Resource, Endpoint, SyncResult, Credential, etc.
    enums.go                  # ResourceType, PushMode, SyncStatus, CredentialType
    transporter.go            # Transporter interface (outbound port)
    scanner.go                # Scanner interface (outbound port)
    credential_store.go       # CredentialStore interface (outbound port)
    errors.go                 # Sentinel errors and custom error types

  transport/                  # Transport factory and implementations
    factory.go                # Factory selecting transporter by resource type
    image/                    # Container image sync (containers/image v5)
    helm/                     # Helm chart sync (Helm v4 SDK)
    git/                      # Git repo sync (go-git v5)

  scanner/                    # Generic external command scanner
    scanner.go                # CommandScanner implementing domain.Scanner

  sync/                       # Orchestration engine
    engine.go                 # Iterates resources, coordinates transports + scanners
    result.go                 # Result aggregation and formatting helpers

  logging/                    # Structured logging setup
    logger.go                 # slog configuration with JSON handler

docker/                       # Dockerfiles (main, scanner variants)
k8s/                          # Kubernetes manifests (Job, CronJob, ConfigMap, Secret)
templates/                    # GitLab CI component template
docs/                         # Extended documentation
  architecture/               # C4 model and detailed design
```

## Architecture

Universal Airgapper follows **Hexagonal Architecture (Ports & Adapters)**:

- **Domain core** (`internal/domain/`) defines types, interfaces, and errors with zero external dependencies.
- **Inbound adapters** (`internal/cli/`, `internal/config/`) translate CLI input and YAML config into domain objects.
- **Outbound adapters** (`internal/transport/*`, `internal/scanner/`, `internal/credentials/`) implement domain interfaces.
- Dependencies always point **inward** -- adapters import domain, never the reverse.

The **Strategy pattern** allows each resource type (image, helm, git) to have its own Transporter implementation. The **Factory pattern** selects the right transporter at runtime.

For the full architecture documentation, see:
- [docs/architecture/c4-model.md](docs/architecture/c4-model.md) -- C4 architecture diagrams
- [docs/architecture/design.md](docs/architecture/design.md) -- detailed design decisions

## Coding Standards

### Formatting and Linting

- Run `gofmt` (or `goimports`) on every file -- no exceptions.
- Use `golangci-lint run` as the single linting command.
- The CI pipeline enforces both.

### Style

- Follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Exported names get doc comments starting with the name:
  ```go
  // Client manages connections to a container registry.
  type Client struct { ... }
  ```
- Use descriptive variable names. Avoid single-letter names outside very short scopes (loop indices, etc.).
- Keep interfaces small -- 1 to 3 methods.

### Error Handling

- Always handle errors immediately. Never discard with `_` unless you document why.
- Wrap errors with context: `fmt.Errorf("pull %s: %w", ref, err)`.
- Use sentinel errors (`var ErrNotFound = errors.New("not found")`) for expected conditions.
- Log errors at the top of the call stack (CLI layer). Lower packages return errors.

### General

- Use `context.Context` as the first parameter for any function that does I/O.
- Avoid `init()` functions; prefer explicit initialization.
- Use struct embedding for composition, not inheritance-style hierarchies.

## Testing

### Running Tests

```shell
go test ./...                     # All unit tests
go test -race ./...               # With race detector
go test -tags=integration ./...   # Integration tests (require external services)
go test -v -run TestFoo ./...     # Run a specific test
```

### Guidelines

- Place tests alongside source: `foo.go` -> `foo_test.go`.
- Use **table-driven tests** as the default pattern:
  ```go
  func TestParseImageEndpoint(t *testing.T) {
      t.Parallel()
      tests := []struct {
          name string
          input string
          want  domain.Endpoint
      }{
          {
              name:  "full reference",
              input: "registry.example.com/repo/image",
              want:  domain.Endpoint{Registry: "registry.example.com", Repository: "repo/image"},
          },
          // ...
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              t.Parallel()
              got := parseImageEndpoint(tt.input)
              assert.Equal(t, tt.want, got)
          })
      }
  }
  ```
- Use `t.Parallel()` for independent tests.
- Use `t.Helper()` in test helper functions.
- Use `testify/assert` and `testify/require` for assertions.
- Mock external services via interfaces, not concrete types. Use `httptest.NewServer` for HTTP dependencies.
- Gate integration tests behind `//go:build integration`.

## Adding a New Transport Type

To add support for a new artifact type (e.g., OPA bundles):

1. **Define the resource type** in `internal/domain/enums.go`:
   ```go
   const ResourceTypeOPA ResourceType = "opa"
   ```
   Update the `Valid()` method.

2. **Implement the Transporter interface** in a new package `internal/transport/opa/`:
   ```go
   package opa

   type Transporter struct { ... }

   func New(logger *slog.Logger) *Transporter { ... }

   func (t *Transporter) Type() domain.ResourceType { return domain.ResourceTypeOPA }
   func (t *Transporter) Sync(ctx context.Context, resource domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) { ... }
   func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) { ... }
   func (t *Transporter) ListVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) { ... }
   ```

3. **Register in the CLI** (`internal/cli/sync.go`):
   ```go
   opaT := opa.New(logger)
   factory := transport.NewFactory(imageT, helmT, gitT, opaT)
   ```

4. **Add config mapping** in `internal/config/types.go` in the `ToResource()` method.

5. **Write tests** for the new transporter.

## Adding a New Scanner

Scanners are **generic** -- no tool-specific code is needed. Just configure a new scanner in your YAML config:

```yaml
scanners:
  - name: snyk
    command: "snyk container test {registry}/{repository}:{tag} --severity-threshold=high"
    success_code: 0
    timeout: 600
```

Then reference it in a resource:

```yaml
resources:
  - type: image
    source: ubuntu
    destination: internal-registry.corp/mirror/ubuntu
    scanner_ref: snyk
    tags:
      - "22.04"
```

No code changes required.

## Commit Conventions

We use conventional commits. Commit messages must match this pattern:

```
^(feat|fix|try|maintain)!?(\(.*\))?: .+
```

### Types

| Type       | When to use                                    |
|------------|------------------------------------------------|
| `feat`     | A new feature                                  |
| `fix`      | A bug fix                                      |
| `try`      | Experimental changes                           |
| `maintain` | Maintenance: refactoring, dependencies, CI, docs |

### Format

```
<type>[optional scope][optional !]: <description>
```

### Examples

```
feat: add OPA bundle transport type
fix(helm): resolve OCI auth token refresh
maintain(deps): update go-git to v5.13
feat!: redesign credential resolution API
maintain(ci): add govulncheck to pipeline
```

A breaking change indicator `!` signals backward-incompatible changes.

## Pull Request Guidelines

1. **One concern per PR.** Keep changes focused.
2. **Branch from `main`**, use the naming convention `task/<description>`.
3. **Ensure all tests pass**: `go test -race ./...`
4. **Ensure lint passes**: `golangci-lint run`
5. **Write tests** for new features and bug fixes.
6. **Update documentation** when adding or changing user-facing behavior.
7. In the PR description, clearly state:
   - What you changed
   - Why you changed it
   - How to test it
   - Any potential side effects

## Code of Conduct

Please be respectful and considerate of others when contributing. We aim to foster an inclusive and welcoming community.
