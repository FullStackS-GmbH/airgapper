# Universal Airgapper - Detailed Design Document

This document complements the [C4 Architecture Model](c4-model.md) with detailed design decisions, interface contracts, data flows, configuration schemas, and implementation guidelines.

---

## Table of Contents

1. [Design Philosophy](#design-philosophy)
2. [Layered Architecture](#layered-architecture)
3. [Building Blocks](#building-blocks)
4. [Interface Contracts](#interface-contracts)
5. [Data Flow](#data-flow)
6. [Configuration Design](#configuration-design)
7. [Credential Management](#credential-management)
8. [Transport Layer Design](#transport-layer-design)
9. [Scanner Integration](#scanner-integration)
10. [Error Handling Strategy](#error-handling-strategy)
11. [Logging Strategy](#logging-strategy)
12. [Concurrency Model](#concurrency-model)
13. [Testing Strategy](#testing-strategy)
14. [CLI Design](#cli-design)
15. [Deployment Model](#deployment-model)
16. [Project Directory Layout](#project-directory-layout)

---

## Design Philosophy

1. **Simplicity over cleverness.** Prefer straightforward Go idioms over abstractions. Three similar lines of code are better than a premature abstraction.
2. **Interfaces at boundaries.** Define small interfaces (1-3 methods) where components interact. Define them where they are consumed, not where they are implemented.
3. **Dependencies point inward.** The domain core has zero external dependencies. Adapters import domain, never the reverse.
4. **Fail fast, fail loud.** Validate config at startup. Return errors immediately. Never swallow errors silently.
5. **Testability by design.** Every component is testable in isolation through interfaces and constructor injection.
6. **Configuration over code.** Adding a new scanner, changing push behavior, or adjusting auth should require config changes - not code changes.

---

## Layered Architecture

The application consists of four distinct layers, ordered from outermost to innermost:

```
┌──────────────────────────────────────────────────────────────┐
│  Layer 1: PRESENTATION (Inbound Adapters)                    │
│  CLI commands (cobra), argument parsing, output formatting   │
│  Packages: internal/cli/                                     │
├──────────────────────────────────────────────────────────────┤
│  Layer 2: APPLICATION (Use Cases / Orchestration)            │
│  Sync engine, workflow coordination, result aggregation      │
│  Packages: internal/sync/, internal/config/                  │
├──────────────────────────────────────────────────────────────┤
│  Layer 3: DOMAIN (Core Business Types)                       │
│  Resource, Credential, SyncResult, interfaces, errors        │
│  Packages: internal/domain/                                  │
├──────────────────────────────────────────────────────────────┤
│  Layer 4: INFRASTRUCTURE (Outbound Adapters)                 │
│  Registry clients, git operations, scanner execution, creds  │
│  Packages: internal/transport/*, internal/scanner/,          │
│            internal/credentials/                             │
└──────────────────────────────────────────────────────────────┘
```

**Dependency rule**: Each layer may only depend on layers below it.
Layer 3 (Domain) depends on nothing. Layer 4 (Infrastructure) implements interfaces defined in Layer 3.

---

## Building Blocks

### Block 1: CLI Layer (`internal/cli/`)

**Purpose**: Translate user intent (CLI arguments, environment variables) into application actions.

**Components**:

- `root.go` - Root cobra command. Defines global persistent flags: `--config`, `--credentials`, `--debug`, `--dry-run`. Binds flags to viper. Initializes logging.
- `sync.go` - `sync` subcommand. Loads config, creates credential store, builds transporter factory, creates sync engine, runs engine, prints results, returns exit code.
- `helm.go` - `helm` subcommand group. Parent command for Helm-related utilities.
- `helm_images.go` - `helm images` subcommand. Pulls and renders Helm charts, extracts image references, writes an airgapper image config YAML ready for `airgapper sync`.
- `version.go` - `version` subcommand. Prints version, commit SHA, build date (injected via ldflags).

**Design pattern**: Command pattern (each cobra subcommand encapsulates a complete action).

**Key decisions**:

- The CLI layer is the **composition root** - it wires all dependencies together.
- The CLI layer is the **only place** that logs errors to the user. Lower layers return errors.
- Exit code mapping: all resources synced → 0, any failure → 1, config/usage error → 2.

### Block 2: Config Loader (`internal/config/`)

**Purpose**: Discover, load, merge, and validate YAML configuration files.

**Components**:

- `loader.go` - Discovers `*.airgapper.yaml` / `*.airgapper.yml` files in the config folder. Merges multiple files by appending resource lists. Uses viper for YAML parsing.
- `types.go` - Typed Go structs for config file content (mirrors the YAML schema).
- `validate.go` - Validates the merged config: required fields, valid enum values, valid regex patterns, no conflicting settings.

**Merge strategy**: When multiple config files are found, their `resources` arrays are concatenated. Scanner definitions and credential definitions are merged by name (last wins with a warning on duplicates).

**Config file naming**: Files must match `*.airgapper.yaml` or `*.airgapper.yml`. This replaces the Python version's `*.cnairgapper.yaml` pattern - but we also accept `*.cnairgapper.yaml` for backward compatibility.

### Block 3: Domain Core (`internal/domain/`)

**Purpose**: Define the language of the application - types, interfaces, errors. This is the heart of the hexagonal architecture.

**Components**:

- `types.go` - `Resource`, `Endpoint`, `SyncResult`, `VersionResult`, `ArtifactRef`, `ScanResult`, `Credential`.
- `enums.go` - `ResourceType`, `PushMode`, `SyncStatus`, `CredentialType`.
- `transporter.go` - `Transporter` interface.
- `scanner.go` - `Scanner` interface.
- `credential_store.go` - `CredentialStore` interface.
- `errors.go` - Sentinel errors and custom error types.

**Zero-dependency rule**: This package imports only the Go standard library. No third-party packages. No other internal packages.

### Block 4: Sync Engine (`internal/sync/`)

**Purpose**: Orchestrate the sync workflow across all resources.

**Components**:

- `engine.go` - Main orchestrator. Iterates resources, selects transporters, runs scanners, aggregates results.
- `result.go` - Result aggregation and reporting helpers.

**Workflow per resource**:

```
For each resource:
  1. Select transporter by resource type (factory lookup)
  2. Expand version patterns (regex matching against remote)
  3. For each version:
     a. Check if version exists at destination (transporter.Exists())
     b. If exists AND push_mode=skip → mark as Skipped
     c. If scanner configured → run scanner on source artifact
        - If scan fails → mark as Failed, continue to next version
     d. If dry-run → log "would sync", mark as Skipped
     e. Call transporter.Sync() → mark as Synced or Failed
  4. Aggregate results for the resource
```

**Concurrency**: Resources are synced concurrently using `errgroup.Group` with a configurable concurrency limit. Versions within a single resource are synced sequentially to avoid overwhelming a single registry.

### Block 5: Transport Layer (`internal/transport/`)

**Purpose**: Provide pluggable implementations for syncing each artifact type.

**Sub-packages**:

- `factory.go` - Transport factory with registered constructors.
- `image/` - Container image transporter.
- `helm/` - Helm chart transporter.
- `git/` - Git repository transporter.

Each sub-package implements the `domain.Transporter` interface. See [Transport Layer Design](#transport-layer-design) for details.

### Block 6: Credential Store (`internal/credentials/`)

**Purpose**: Load credentials from YAML files and resolve them by host or reference name.

**Components**:

- `store.go` - `FileStore` struct implementing `domain.CredentialStore`. Loads YAML files, indexes credentials by name and type.
- `resolver.go` - Resolution logic: match by explicit reference name, or fall back to hostname matching.

See [Credential Management](#credential-management) for details.

### Block 7: External Scanner (`internal/scanner/`)

**Purpose**: Run external security/compliance commands on artifacts before promotion.

**Components**:

- `scanner.go` - `CommandScanner` struct implementing `domain.Scanner`. Executes a parameterized shell command, checks exit code, captures output.

See [Scanner Integration](#scanner-integration) for details.

### Block 8: Logging (`internal/logging/`)

**Purpose**: Configure structured logging with `slog`.

**Components**:

- `logger.go` - Creates `slog.Logger` with JSON handler. Sets log level from config. Provides helper for creating child loggers with contextual attributes.

---

## Interface Contracts

### Transporter Interface

```go
type Transporter interface {
    // Type returns the resource type this transporter handles.
    Type() ResourceType

    // Sync copies a single versioned artifact from source to destination.
    // The version field in the resource identifies which specific version to sync.
    // Returns a SyncResult with the outcome for each version.
    Sync(ctx context.Context, resource Resource, opts SyncOptions) (*SyncResult, error)

    // Exists checks whether a specific version exists at the given endpoint.
    Exists(ctx context.Context, endpoint Endpoint, version string, creds *Credential) (bool, error)

    // ListVersions returns all available versions/tags/refs at the given endpoint.
    // Used for regex pattern expansion against the remote.
    ListVersions(ctx context.Context, endpoint Endpoint, creds *Credential) ([]string, error)
}
```

**Contract rules**:

- `Sync` must be idempotent: calling it twice with the same input produces the same result.
- `Sync` must respect `SyncOptions.DryRun` - when true, it must not modify the destination.
- `Exists` must not modify any state.
- `ListVersions` must return all available versions, unsorted. The caller handles filtering and regex matching.
- All methods must respect `ctx` cancellation.

### Scanner Interface

```go
type Scanner interface {
    // Name returns the scanner identifier (matches config reference).
    Name() string

    // Scan runs the configured check against the given artifact.
    // Returns a ScanResult with pass/fail and captured output.
    Scan(ctx context.Context, artifact ArtifactRef) (*ScanResult, error)
}
```

**Contract rules**:

- `Scan` must not modify the artifact or any registry state.
- `Scan` returns `ScanResult.Passed = true` if the exit code matches the configured success code.
- If the scanner command cannot be executed (binary not found, permission denied), return an error.
- If the scanner command executes but fails, return a `ScanResult` with `Passed = false`, not an error.

### CredentialStore Interface

```go
type CredentialStore interface {
    // Resolve finds credentials matching the given host and type.
    // Returns nil (not an error) if no credentials are found.
    Resolve(host string, credType CredentialType) (*Credential, error)

    // ResolveByRef finds credentials by their reference name.
    // Returns ErrNotFound if the reference does not exist.
    ResolveByRef(ref string) (*Credential, error)
}
```

**Contract rules**:

- `Resolve` returns `nil, nil` when no matching credential exists (anonymous access).
- `ResolveByRef` returns `domain.ErrNotFound` when the reference does not exist.
- Credentials must never be logged. The store must not expose secrets in error messages.

---

## Data Flow

### Happy Path: Sync All Resources

```
User runs: airgapper sync --config ./configs/ --credentials ./creds/

1. CLI Layer
   ├─ Parse flags, bind to viper
   ├─ Initialize slog (JSON, level from --debug)
   ├─ config.Load("./configs/")
   │   ├─ Discover *.airgapper.yaml files
   │   ├─ Parse and merge YAML
   │   └─ Validate merged config → Config struct
   ├─ credentials.NewFileStore("./creds/")
   │   ├─ Load credential YAML files
   │   └─ Index by name and type
   ├─ transport.NewFactory(logger)
   │   ├─ Register image.New()
   │   ├─ Register helm.New()
   │   └─ Register git.New()
   ├─ scanner.NewFromConfig(config.Scanners)
   └─ sync.NewEngine(factory, scanners, logger)

2. Sync Engine
   ├─ For each resource (concurrently via errgroup):
   │   ├─ factory.ForType(resource.Type) → transporter
   │   ├─ Expand version patterns:
   │   │   ├─ transporter.ListVersions(source) → all versions
   │   │   └─ Filter by regex patterns → expanded versions
   │   ├─ For each version (sequentially):
   │   │   ├─ transporter.Exists(destination, version) → bool
   │   │   ├─ If exists && push_mode=skip → VersionResult{Skipped}
   │   │   ├─ If scanner configured:
   │   │   │   └─ scanner.Scan(artifact) → ScanResult
   │   │   │       └─ If !passed → VersionResult{Failed}
   │   │   ├─ If dry_run → VersionResult{Skipped, "dry-run"}
   │   │   └─ transporter.Sync(resource, version) → VersionResult{Synced}
   │   └─ Aggregate → SyncResult
   └─ Collect all SyncResults

3. CLI Layer
   ├─ Print summary table (synced / skipped / failed per resource)
   ├─ If any failed → exit 1
   └─ Else → exit 0
```

### Error Path: Authentication Failure

```
1. transporter.Sync() attempts to pull from source registry
2. Source registry returns 401 Unauthorized
3. Transporter wraps error: fmt.Errorf("pull %s:%s: %w", repo, tag, ErrAuthFailed)
4. Sync engine records VersionResult{Failed, error}
5. Engine continues with remaining versions/resources
6. CLI layer prints failure in summary, exits with code 1
```

---

## Configuration Design

### Config File Schema

```yaml
# *.airgapper.yaml

# Optional: scanner definitions (generic external commands)
scanners:
  - name: "trivy"
    command: "trivy image --exit-code 1 --severity HIGH,CRITICAL {registry}/{repository}:{tag}"
    success_code: 0          # Expected exit code for "scan passed" (default: 0)
    timeout: 300             # Command timeout in seconds (default: 300)

# Required: resources to sync
resources:
  # Container image
  - type: image
    source: "registry.example.com/team/app"
    destination: "internal-registry.corp/mirror/app"
    source_credentials_ref: "source-registry"    # Optional
    target_credentials_ref: "internal-registry"   # Optional
    push_mode: "skip"                             # skip | force (default: skip)
    scanner_ref: "trivy"                          # Optional scanner reference
    tags:
      - "v1.0.0"
      - "v1.0.1"
      - "v2\\..*"     # Regex: all v2.x tags

  # Helm chart
  - type: helm
    source_registry: "registry-1.docker.io"
    source_chart: "bitnamicharts/postgresql"
    destination_registry: "internal-registry.corp"
    destination_repo: "charts"
    push_mode: "skip"                             # skip | overwrite (default: skip)
    versions:
      - "16.0.0"
      - "16\\..*"     # Regex: all 16.x versions

  # Git repository
  - type: git
    source_repo: "git@github.com:org/project.git"
    destination_repo: "git@internal-git.corp:mirror/project.git"
    source_credentials_ref: "github-ssh"          # Optional
    target_credentials_ref: "internal-git-ssh"    # Optional
    push_mode: "skip"                             # skip | push | force (default: skip)
    refs:
      - "main"
      - "release/.*"  # Regex: all release branches
      - "v[0-9]+\\..*" # Regex: all version tags
```

### Config Struct (Go)

```go
type Config struct {
    Scanners  []ScannerConfig  `mapstructure:"scanners"`
    Resources []ResourceConfig `mapstructure:"resources"`
}

type ScannerConfig struct {
    Name        string `mapstructure:"name" validate:"required"`
    Command     string `mapstructure:"command" validate:"required"`
    SuccessCode int    `mapstructure:"success_code"`
    Timeout     int    `mapstructure:"timeout"`
}

type ResourceConfig struct {
    Type                 string   `mapstructure:"type" validate:"required,oneof=image helm git"`
    // Image fields
    Source               string   `mapstructure:"source"`
    Destination          string   `mapstructure:"destination"`
    Tags                 []string `mapstructure:"tags"`
    // Helm fields
    SourceRegistry       string   `mapstructure:"source_registry"`
    SourceChart          string   `mapstructure:"source_chart"`
    DestinationRegistry  string   `mapstructure:"destination_registry"`
    DestinationRepo      string   `mapstructure:"destination_repo"`
    Versions             []string `mapstructure:"versions"`
    // Git fields
    SourceRepo           string   `mapstructure:"source_repo"`
    DestinationRepo_     string   `mapstructure:"destination_repo"` // shared with Helm
    Refs                 []string `mapstructure:"refs"`
    // Common fields
    PushMode             string   `mapstructure:"push_mode"`
    ScannerRef           string   `mapstructure:"scanner_ref"`
    SourceCredentialsRef string   `mapstructure:"source_credentials_ref"`
    TargetCredentialsRef string   `mapstructure:"target_credentials_ref"`
    // Legacy field names (backward compat)
    Scan                 any      `mapstructure:"scan"`
}
```

### Regex Pattern Detection

A version/tag/ref string is treated as a regex pattern if it contains any of: `*`, `+`, `[`, `]`, `{`, `}`, `(`, `)`, `\`, `^`, `$`, `|`, `?`.

SemVer values with build metadata, such as `108.0.1+up2.8.10`, are treated as literal versions even though they contain `+`.

When a pattern is detected:

1. Call `transporter.ListVersions()` to get all available versions from the source.
2. Compile the pattern as a Go `regexp`.
3. Filter the version list, returning only full matches (`regexp.MatchString` anchored with `^...$`).
4. Add the matched versions to the sync list.

---

## Credential Management

### Credential File Schema

```yaml
# credentials.yaml (or multiple files in a folder)

image:
  - name: "docker-hub"
    username: "myuser"
    password: "mytoken"
  - name: "internal-registry"
    username: "serviceaccount"
    password: "s3cret"

helm:
  - name: "chart-registry"
    username: "helmuser"
    password: "helmpass"

git:
  - name: "github-ssh"
    ssh_key_path: "/path/to/id_ed25519"
  - name: "github-https"
    username: "git"
    password: "ghp_xxxxxxxxxxxx"
  - name: "azure-repos"
    username: "AzureReposAuthnSucks"
    password: "REPO_AUTH_HEADER"   # Env var name containing the auth header
```

### Resolution Algorithm

1. **By reference**: If a resource specifies `source_credentials_ref` or `target_credentials_ref`, look up the credential by name. Error if not found.
2. **By hostname**: If no explicit reference, extract the hostname from the endpoint URL and search for a credential whose `name` matches the hostname.
3. **Anonymous**: If no credential is found, proceed with anonymous access (no auth headers).

### Azure Repos Workaround

When `username` equals `"AzureReposAuthnSucks"`, the `password` field is interpreted as an environment variable name.
The git transporter reads that env var to obtain the `Authorization` header for HTTPS authentication.

### Security Rules

- Credentials are never logged, even at DEBUG level.
- Credential values are masked in error messages (show only the credential name, never the password).
- The credential file path is logged at INFO level for operational visibility.
- File permissions on credential files are not enforced by the tool but documented as a security recommendation.

---

## Transport Layer Design

### Image Transporter (`internal/transport/image/`)

**Library**: `go.podman.io/image/v5` (same engine family as skopeo and podman)

**How it works**:

1. **Parse references**: Convert source/destination strings to image references using the Docker transport.
2. **Create SystemContext**: Set auth credentials, TLS settings, and transport options.
3. **Copy**: Call `copy.Image(ctx, policyContext, destRef, srcRef, &copy.Options{...})`.
    - The library handles manifest fetching, layer transfer, blob reuse, multi-arch copy, digest verification, auth challenges, and retries.
4. **Existence check**: Resolve the destination digest for the requested tag.
5. **List versions**: Read registry tags through the Docker transport.

**Image name parsing** (same logic as the Python version):

- `ubuntu` → `registry-1.docker.io/library/ubuntu`
- `myrepo/myimage` → `registry-1.docker.io/myrepo/myimage`
- `registry.example.com/myrepo/myimage` → as-is

**Multi-arch handling**: By default, copy all platforms from a manifest list. If the source has a multi-arch manifest, the destination receives the same multi-arch manifest.

### Helm Transporter (`internal/transport/helm/`)

**Library**: `helm.sh/helm/v4/pkg/action` (Helm v4 SDK)

**OCI registries** (e.g., Docker Hub, Harbor, GHCR):

1. **Pull**: Use the Helm SDK's `registry.Client` to pull the chart as an OCI artifact.
2. **Push**: Use the Helm SDK's `registry.Client` to push the chart to the destination OCI registry.
3. **Existence check**: HEAD request to the OCI manifest endpoint.
4. **List versions**: GET `/v2/{name}/tags/list`.

**Legacy HTTP registries** (e.g., ChartMuseum, Nexus):

1. **Pull**: Fetch `index.yaml`, find the chart URL, download the `.tgz` archive.
2. **Push**: Upload the `.tgz` via the repository's upload API (Nexus uses a different endpoint than ChartMuseum).
3. **Existence check**: Parse `index.yaml` for the version.
4. **List versions**: Parse `index.yaml` for all chart versions.

**Registry type detection**: If the registry URL starts with `oci://` or resolves to an OCI distribution API, use OCI mode. If the URL points to an `index.yaml`-based repository (detected by fetching `/index.yaml`), use legacy mode.

### Git Transporter (`internal/transport/git/`)

**Library**: `github.com/go-git/go-git/v5`

**Workflow per ref**:

1. **Clone**: Clone the source repository to a temp directory, checking out the specific ref.
    - For HTTPS: embed credentials in the URL or use `http.BasicAuth`.
    - For SSH: use `ssh.PublicKeys` with the private key from the credential store.
2. **Add remote**: Add the destination as a second remote (`target`).
3. **Push**: Push the ref to the `target` remote.
    - `skip`: Check if ref exists at destination first. If yes, skip.
    - `push`: Standard push. Fails if destination ref is ahead.
    - `force`: Force push (equivalent to `git push --force`).
4. **Cleanup**: Remove the temp directory.

**Ref pattern matching**:

1. Call `git ls-remote` on the source repository to list all refs.
2. For each ref, check if any configured pattern matches (using the regex detection logic).
3. Expand patterns into concrete ref names.

**Azure Repos SSH workaround**: See [Credential Management](#credential-management).

---

## Scanner Integration

### Generic Command Scanner

The scanner is a generic external command executor. It replaces all tool-specific scanner implementations from the Python version.

**Configuration**:

```yaml
scanners:
  - name: "trivy"
    command: "trivy image --exit-code 1 --severity HIGH,CRITICAL {registry}/{repository}:{tag}"
    success_code: 0
    timeout: 300
```

**Placeholders** (substituted at runtime):

| Placeholder    | Description                         | Example                                |
|----------------|-------------------------------------|----------------------------------------|
| `{registry}`   | Registry hostname                   | `registry.example.com`                 |
| `{repository}` | Repository path                     | `team/app`                             |
| `{tag}`        | Image tag / chart version / git ref | `v1.0.0`                               |
| `{source}`     | Full source reference               | `registry.example.com/team/app:v1.0.0` |
| `{type}`       | Resource type                       | `image`, `helm`, `git`                 |

**Execution**:

1. Substitute placeholders in the command string.
2. Execute via `os/exec.CommandContext` with the configured timeout.
3. Capture stdout and stderr.
4. Compare exit code against `success_code`.
5. Return `ScanResult{Passed: exitCode == successCode, Output: stdout, Error: stderr, Code: exitCode}`.

**Error handling**:

- Command not found → return Go error (not a ScanResult).
- Command exits with non-success code → return `ScanResult{Passed: false}`.
- Command times out → return Go error with context deadline exceeded.

---

## Error Handling Strategy

### Error Hierarchy

```
errors (sentinel)
├── ErrNotFound          - Resource, version, or credential not found
├── ErrAuthFailed        - Authentication failed (401/403)
├── ErrAlreadyExists     - Version already exists at destination (informational)
├── ErrScanFailed        - Scanner reported a failure
├── ErrInvalidConfig     - Configuration validation failed
├── ErrTimeout           - Operation timed out
└── ErrTransportFailed   - Transport-level failure (network, protocol)

errors (wrapped)
└── fmt.Errorf("sync %s %s:%s: %w", type, repo, version, cause)
```

### Error Propagation Rules

1. **Domain layer**: Define sentinel errors. Never log.
2. **Transport layer**: Wrap errors with context (`fmt.Errorf("pull image %s: %w", ref, err)`). Never log.
3. **Sync engine**: Catch errors, record in `VersionResult.Error`. Log at WARN level. Continue processing.
4. **CLI layer**: Print final summary. Log errors at ERROR level. Set exit code.

### Partial Failure Handling

The sync engine never stops on a single failure. It processes all resources and all versions, recording each outcome. The final exit code reflects whether any failures occurred.

---

## Logging Strategy

### Log Format

JSON structured logging via `log/slog`:

```json
{
  "time": "2025-01-15T10:30:00.000Z",
  "level": "INFO",
  "msg": "synced image",
  "resource.type": "image",
  "resource.source": "docker.io/library/ubuntu",
  "resource.destination": "internal-registry.corp/mirror/ubuntu",
  "resource.version": "22.04",
  "duration_ms": 4523
}
```

### OpenTelemetry Semantic Conventions

Follow [OTel semantic conventions](https://opentelemetry.io/docs/specs/semconv/) for attribute names:

| Attribute     | Convention             | Example                    |
|---------------|------------------------|----------------------------|
| Resource type | `resource.type`        | `image`                    |
| Source        | `resource.source`      | `docker.io/library/ubuntu` |
| Destination   | `resource.destination` | `internal.corp/ubuntu`     |
| Version       | `resource.version`     | `22.04`                    |
| Duration      | `duration_ms`          | `4523`                     |
| Error         | `error.message`        | `authentication failed`    |
| Registry      | `registry.host`        | `registry-1.docker.io`     |

### Log Levels

| Level   | Usage                                                                                |
|---------|--------------------------------------------------------------------------------------|
| `DEBUG` | Transport protocol details, HTTP requests/responses (redacted), config parsing steps |
| `INFO`  | Resource sync started/completed, version synced/skipped, scanner results             |
| `WARN`  | Version sync failed (non-fatal), deprecated config fields, duplicate credentials     |
| `ERROR` | Fatal errors that prevent the tool from operating (config invalid, no resources)     |

### Debug Mode

When `--debug` is set:

- Log level changes from INFO to DEBUG.
- All HTTP request/response headers are logged (with credential values masked).
- Config parsing steps are logged.
- Scanner command and full output are logged.

---

## Concurrency Model

### Resource-Level Parallelism

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(maxConcurrent) // Configurable, default: 4

for _, resource := range resources {
    resource := resource // capture loop variable
    g.Go(func() error {
        result, err := e.syncResource(ctx, resource, opts)
        // ... record result
        return nil // never propagate error to errgroup; record in results
    })
}

if err := g.Wait(); err != nil {
    // Only context cancellation errors reach here
}
```

### Version-Level Sequentiality

Within a single resource, versions are synced sequentially. Reasons:

1. Avoid overwhelming a single registry with parallel requests.
2. Registry rate limits are per-host; parallel version requests to the same host may trigger throttling.
3. The order of operations matters for some registries (e.g., manifest list before individual platforms).

### Context Cancellation

All operations respect `context.Context`. A SIGINT/SIGTERM cancels the root context, which propagates to all in-flight operations. Each transporter must check `ctx.Done()` and return promptly.

---

## Testing Strategy

### Unit Tests

Every package gets `_test.go` files alongside the source. Table-driven tests are the default pattern.

**Domain types**: Test validation, equality, string formatting.
**Config loader**: Test YAML parsing, merging, validation (use testdata files).
**Credential store**: Test resolution by name, by host, anonymous fallback.
**Sync engine**: Test orchestration logic with mock transporters and scanners.
**Scanner**: Test placeholder substitution, exit code interpretation.

### Mock Strategy

Hand-written mocks implementing domain interfaces:

```go
type MockTransporter struct {
    TypeFunc         func() ResourceType
    SyncFunc         func(ctx context.Context, r Resource, opts SyncOptions) (*SyncResult, error)
    ExistsFunc       func(ctx context.Context, ep Endpoint, version string, creds *Credential) (bool, error)
    ListVersionsFunc func(ctx context.Context, ep Endpoint, creds *Credential) ([]string, error)
}
```

No mocking frameworks - keep it simple and explicit.

### Integration Tests

Behind `//go:build integration` build tag:

- Spin up a local Docker registry (`testcontainers-go`) to test image sync.
- Spin up a local ChartMuseum to test Helm chart sync.
- Use a local bare git repository (in `t.TempDir()`) to test git sync.
- Test full end-to-end flow: config → sync engine → real registries.

### Test Commands

```bash
go test ./...                           # Unit tests only
go test -race ./...                     # Unit tests with race detection
go test -tags integration ./...         # Unit + integration tests
go test -v -run TestSyncEngine ./...    # Specific test
```

---

## CLI Design

### Command Structure

```
airgapper
├── sync              Synchronize artifacts from source to destination
├── helm
│   └── images        Extract container image references from Helm charts
├── version           Print version information
└── help              Help about any command

Global flags:
  --config, -c       Path to config file or folder (env: AIRGAPPER_CONFIG)
  --credentials      Path to credentials file or folder (env: AIRGAPPER_CREDENTIALS)
  --debug, -d        Enable debug logging (env: AIRGAPPER_DEBUG)
  --dry-run          Disable all write/push operations (env: AIRGAPPER_DRY_RUN)
  --help, -h         Help for airgapper
```

### Exit Codes

| Code | Meaning                                                      |
|------|--------------------------------------------------------------|
| 0    | All resources synced successfully (or skipped per push_mode) |
| 1    | At least one resource/version failed to sync                 |
| 2    | Configuration or usage error (invalid flags, missing config) |

### Output

**Normal mode**: One line per version showing status.

```
[image] docker.io/library/ubuntu:22.04 → internal.corp/ubuntu:22.04  SYNCED
[image] docker.io/library/ubuntu:20.04 → internal.corp/ubuntu:20.04  SKIPPED (exists)
[helm]  docker.io/bitnamicharts/postgresql:16.0.0 → internal.corp/charts:16.0.0  SYNCED
[git]   github.com:org/project main → internal-git:mirror/project main  FAILED (auth)

Summary: 2 synced, 1 skipped, 1 failed
```

**Debug mode**: Full JSON structured logs interleaved with status output.

**Dry-run mode**: Prefix all lines with `[DRY-RUN]` and skip all write operations.

---

## Deployment Model

### Local CLI

```bash
# Install
go install github.com/fullstacks-gmbh/universal-airgapper/cmd/airgapper@latest

# Run
airgapper sync --config ./configs/ --credentials ./creds/
```

### Container Image

```dockerfile
# Multi-stage build
FROM golang:1.26.3-alpine AS builder
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
    -X main.version=${APP_VERSION} \
    -X main.commit=${APP_COMMIT_SHA}" \
    -o /airgapper ./cmd/airgapper

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /airgapper /airgapper
ENTRYPOINT ["/airgapper"]
```

### Kubernetes Job / CronJob

See `k8s/job.yaml` and `k8s/cronJob.yaml` - mount ConfigMaps for config, Secrets for credentials.

### GitHub Actions (Reusable Workflow)

See `.github/workflows/universal-airgapper.yml` - runs the container image with config from the caller's repository.

### GitLab CI (Component Template)

See `templates/airgapper.yml` - parameterized GitLab CI job using the container image.

---

## Project Directory Layout

```
universal-airgapper-golang/
├── cmd/
│   └── airgapper/
│       └── main.go                  # Thin entrypoint
├── internal/
│   ├── domain/
│   │   ├── types.go                 # Resource, Endpoint, SyncResult, etc.
│   │   ├── enums.go                 # ResourceType, PushMode, SyncStatus, etc.
│   │   ├── transporter.go           # Transporter interface
│   │   ├── scanner.go               # Scanner interface
│   │   ├── credential_store.go      # CredentialStore interface
│   │   └── errors.go                # Sentinel errors
│   ├── config/
│   │   ├── loader.go                # YAML discovery, parsing, merging
│   │   ├── types.go                 # Config structs (mapstructure tags)
│   │   ├── validate.go              # Config validation
│   │   ├── loader_test.go
│   │   ├── validate_test.go
│   │   └── testdata/                # Test YAML fixtures
│   ├── credentials/
│   │   ├── store.go                 # FileStore implementation
│   │   ├── resolver.go              # Credential resolution logic
│   │   ├── store_test.go
│   │   └── testdata/
│   ├── transport/
│   │   ├── factory.go               # Transport factory + registry
│   │   ├── factory_test.go
│   │   ├── image/
│   │   │   ├── transporter.go       # Image sync via containers/image v5
│   │   │   ├── parse.go             # Image name parsing
│   │   │   ├── transporter_test.go
│   │   │   └── parse_test.go
│   │   ├── helm/
│   │   │   ├── transporter.go       # Helm chart sync via Helm v4 SDK
│   │   │   ├── registry.go          # OCI vs legacy detection
│   │   │   ├── transporter_test.go
│   │   │   └── registry_test.go
│   │   └── git/
│   │       ├── transporter.go       # Git repo sync via go-git
│   │       ├── pattern.go           # Regex ref matching
│   │       ├── transporter_test.go
│   │       └── pattern_test.go
│   ├── scanner/
│   │   ├── scanner.go               # CommandScanner implementation
│   │   └── scanner_test.go
│   ├── sync/
│   │   ├── engine.go                # Sync orchestrator
│   │   ├── result.go                # Result aggregation
│   │   ├── engine_test.go
│   │   └── result_test.go
│   ├── helmimages/
│   │   ├── extractor.go             # Chart rendering, image extraction, output YAML
│   │   └── extractor_test.go
│   ├── logging/
│   │   └── logger.go                # slog setup
│   └── cli/
│       ├── root.go                  # Root command + global flags
│       ├── sync.go                  # Sync subcommand
│       ├── helm.go                  # Helm subcommand group
│       ├── helm_images.go           # helm images subcommand
│       ├── helm_images_test.go
│       └── version.go               # Version subcommand
├── docker/
│   └── Dockerfile                   # Multi-stage Go build → scratch
├── k8s/
│   ├── job.yaml                     # Kubernetes Job manifest
│   └── cronJob.yaml                 # Kubernetes CronJob manifest
├── templates/
│   └── airgapper.yml                # GitLab CI component template
├── .github/
│   └── workflows/
│       ├── ci.yml                   # Build, lint, test on push/PR
│       ├── docker-build.yml         # Build + push Docker image on tags
│       └── universal-airgapper.yml  # Reusable workflow for consumers
├── .gitlab-ci.yml                   # GitLab CI pipeline
├── docs/
│   ├── architecture/
│   │   ├── c4-model.md              # C4 architecture model (this companion)
│   │   └── design.md                # Detailed design document (this file)
│   ├── configuration.md             # Config file reference
│   ├── credentials.md               # Credential management guide
│   └── gitlab-ci.md                 # GitLab CI usage guide
├── .devcontainer/
│   └── devcontainer.json            # VS Code dev container
├── flake.nix                        # Nix flake for dev environment
├── .envrc                           # direnv config
├── go.mod                           # Go module file
├── go.sum                           # Go dependency checksums
├── Makefile                         # Build, lint, test targets
├── .golangci.yml                    # golangci-lint configuration
├── .pre-commit-config.yaml          # Pre-commit hooks
├── .gitignore
├── .dockerignore
├── AGENTS.md                        # AI agent guidelines
├── README.md                        # Project README
├── CONTRIBUTING.md                  # Contribution guide
├── CHANGELOG.md                     # Version history
├── SECURITY.md                      # Security policy
└── LICENSE                          # GPLv3
```
