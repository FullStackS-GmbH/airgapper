# Repository Guidelines — Universal Airgapper (Go)

## Project Overview

Universal Airgapper is a CLI tool for synchronizing artifacts (container images, Helm charts, Git repositories) across registries and hosts — designed for air-gapped environments and automated CI/CD pipelines. This is a Go rewrite of the original Python implementation, following hexagonal architecture principles.

## Architecture

- **Pattern**: Hexagonal Architecture (Ports & Adapters) with Strategy pattern for pluggable transports.
- **Core domain** (`internal/domain/`): Pure business types, interfaces, and errors. Zero external dependencies. Dependencies always point inward — adapters import domain, never the reverse.
- **Inbound ports**: CLI commands (cobra), config file parsing (viper).
- **Outbound ports**: `Transporter` interface (image, helm, git), `Scanner` interface, `CredentialStore` interface.
- **Outbound adapters**: Concrete implementations in `internal/transport/image/`, `internal/transport/helm/`, `internal/transport/git/`, `internal/scanner/`.
- Refer to `docs/architecture/` for the full C4 model and design documentation.

## Project Structure & Module Organization

```
cmd/airgapper/main.go        # Thin entrypoint — wires dependencies, calls root command
internal/
  domain/                     # Shared types, interfaces, sentinel errors
  config/                     # Viper-based config loading, validation, merging
  credentials/                # Credential store: file-based loading and resolution
  transport/                  # Transporter interface + factory/registry
    image/                    # Container image sync (containers/image v5)
    helm/                     # Helm chart sync (Helm v4 SDK)
    git/                      # Git repo sync (go-git/go-git v5)
  scanner/                    # Generic external command scanner
  sync/                       # Orchestration engine — coordinates transports + scanners
  cli/                        # Cobra command definitions (root, sync, version)
  logging/                    # Structured logging setup (slog, JSON, OpenTelemetry semantics)
```

- Each `cmd/<name>/main.go` is a thin wrapper that wires dependencies and calls into `internal/` packages.
- Group by domain concern, not by layer.
- Keep `go.mod` at the repo root with module path `github.com/fullstacks-gmbh/universal-airgapper`.
- Place shared types and interfaces in `internal/domain/` to avoid import cycles.
- Define interfaces where they are consumed, not where they are implemented.

## Required Libraries

| Concern | Library                          | Notes                                 |
|---------|----------------------------------|---------------------------------------|
| CLI     | `github.com/spf13/cobra`         | Subcommand-based CLI                  |
| Config  | `github.com/spf13/viper`         | YAML config + env vars + flags        |
| Git     | `github.com/go-git/go-git/v5`    | Pure-Go git operations                |
| Helm    | `helm.sh/helm/v4/pkg/action`     | Official Helm v4 SDK                  |
| Images  | `github.com/containers/image/v5` | Same engine as skopeo/podman          |
| Logging | `log/slog` (stdlib)              | JSON handler, OpenTelemetry semantics |
| Testing | `github.com/stretchr/testify`    | Assertions and require                |

## Setup, Build, and Run

- Requires Go 1.23+. Build with `go build ./cmd/airgapper`.
- Run locally with `go run ./cmd/airgapper -- --help`.
- Use `go mod tidy` after adding or removing dependencies; commit both `go.mod` and `go.sum`.
- For reproducible builds: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" ./cmd/airgapper`.
- Use `Makefile` to codify build, lint, test, and release targets.
- Dev environment: `direnv allow` loads nix flake with Go toolchain.

## Coding Style & Conventions

- Follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Run `gofmt` (or `goimports`) on every file — no exceptions. Use `golangci-lint run` as the single linting command.
- Exported names get doc comments starting with the name: `// Client manages registry connections.`
- Prefer returning `error` over panicking. Wrap errors with `fmt.Errorf("operation: %w", err)` to preserve context.
- Use `context.Context` as the first parameter for any function that does I/O or may be cancelled.
- Keep interfaces small (1–3 methods).
- Avoid `init()` functions; prefer explicit initialization in `main` or a setup function.
- Use struct embedding for composition, not inheritance-style hierarchies.

## Error Handling

- Always handle errors immediately; never discard with `_` unless documented.
- Use sentinel errors (`var ErrNotFound = errors.New("not found")`) for expected conditions.
- For richer errors, define custom types implementing `error` with `errors.Is`/`errors.As` support.
- Log at the top of the call stack (usually `cmd/`); lower packages return errors, they don't log them.

## CLI Design (cobra + viper)

- Subcommands are verbs: `airgapper sync`, `airgapper version`.
- Global flags: `--config` (config file/folder), `--credentials` (credentials file/folder), `--debug`, `--dry-run`.
- Every flag has a short description, sensible default, and env-var override (`AIRGAPPER_` prefix).
- Exit codes: `0` success, `1` general error, `2` usage/config error.
- `--dry-run` disables all write/push operations globally.
- `--debug` enables verbose (DEBUG level) structured JSON logging.

## Configuration

- Support config file (YAML), environment variables, and CLI flags — flags win.
- Config files: `*.airgapper.yaml` / `*.airgapper.yml` in a folder, merged at load time.
- Validate configuration early at startup; fail fast with clear messages.
- Never log secrets. Mask credentials in any debug or error output.
- Use `viper.AutomaticEnv()` with prefix `AIRGAPPER_` for env-var binding.
- Support regex patterns for image tags, chart versions, and git refs.

## Credential Management

- Credentials stored in separate YAML files, organized by type: `image`, `helm`, `git`.
- Resources reference credentials by name via `source_credentials_ref` / `target_credentials_ref`.
- Support both single file (`--credentials`) and folder of files.
- Support SSH keys for git (path to private key file).
- Support Azure Repos HTTPS workaround (env var containing auth header).

## Transport Layer

- All transports implement the same `Transporter` interface: `Sync(ctx, resource, creds) (*SyncResult, error)`.
- Factory pattern selects the correct transporter based on resource type.
- Each transport handles: existence checking, pulling, pushing, and push-mode logic (skip/force/overwrite).
- Image transport uses `containers/image/v5` copy mechanism (same as skopeo).
- Helm transport uses Helm v4 SDK for OCI and traditional repos.
- Git transport uses `go-git/go-git/v5` for clone/push with HTTPS and SSH support.

## Scanner Integration

- **Generic external command scanner only** — no tool-specific implementations.
- Scanner runs a parameterized external command with placeholders (e.g., `{image}`, `{tag}`, `{registry}`).
- Expects a configurable return code for pass/fail.
- Optionally captures and logs stdout/stderr.
- Scanner config in the resource YAML, referenced by name.

## Testing

- Place tests alongside source files: `foo.go` → `foo_test.go`.
- Use table-driven tests as the default pattern.
- Use `testify/assert` and `testify/require` for assertions.
- Use `t.Helper()` in test helpers, `t.Parallel()` for independent tests.
- Mock external services with interfaces, not concrete types. Use `httptest.NewServer` for HTTP deps.
- Place integration tests behind `//go:build integration`.
- Run: `go test ./...` and `go test -race ./...`.

## Concurrency

- Use `errgroup.Group` for structured concurrent work with error propagation.
- Use `context.Context` for cancellation and timeouts on all concurrent operations.
- Never start goroutines without clear ownership and shutdown path.

## Logging & Observability

- Use `log/slog` with JSON handler for structured logging.
- Follow OpenTelemetry semantic conventions for field names.
- Log levels: `Debug` for diagnostics, `Info` for operational events, `Warn` for recoverable issues, `Error` for failures.
- Include contextual fields (resource name, registry URL, duration) — not interpolated strings.
- Debug mode (`--debug`) sets log level to DEBUG.

## Docker & Release

- Multi-stage builds: build in Go image, copy static binary into `scratch` or `distroless`.
- Ship a single static binary with no CGO dependencies (`CGO_ENABLED=0`).
- Tag releases with semver.
- Build args: `APP_VERSION`, `APP_COMMIT_SHA`.

## CI/CD Integration

- GitHub Actions: reusable workflow for running airgapper in a container.
- GitHub Actions: docker-build workflow for building and pushing images on tags.
- GitLab CI: component template for running airgapper in GitLab pipelines.
- Both CI systems use the same container image with the Go binary.

## Commit & PR Guidelines

- DO NOT COMMIT ANYTHING!

## Security

- Never commit secrets; use credential files, env vars, or secrets manager.
- Validate all external input at system boundaries.
- Use `crypto/tls` with secure defaults; do not disable cert verification outside tests.
- Run `govulncheck ./...` periodically.
- Pre-commit hooks detect AWS credentials and private keys.
