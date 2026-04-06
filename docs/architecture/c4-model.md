# Universal Airgapper — C4 Architecture Model

This document describes the architecture of Universal Airgapper using the [C4 model](https://c4model.com/) (Context, Container, Component, Code).
The Go rewrite follows **Hexagonal Architecture (Ports & Adapters)** combined with the **Strategy** and **Factory** patterns for pluggable transport backends.

---

## Table of Contents

1. [Level 1: System Context](#level-1-system-context)
2. [Level 2: Container Diagram](#level-2-container-diagram)
3. [Level 3: Component Diagram](#level-3-component-diagram)
4. [Level 4: Code Diagram](#level-4-code-diagram)
5. [Architecture Decision Records](#architecture-decision-records)

---

## Level 1: System Context

The System Context diagram shows Universal Airgapper and its relationships to external systems and actors.

```
┌─────────────────────────────────────────────────────────────────────┐
│                        External Systems                             │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │
│  │ Container    │  │ Helm Chart   │  │ Git Hosting  │               │
│  │ Registries   │  │ Registries   │  │ Services     │               │
│  │              │  │              │  │              │               │
│  │ Docker Hub   │  │ Docker Hub   │  │ GitHub       │               │
│  │ GHCR         │  │ GHCR (OCI)   │  │ GitLab       │               │
│  │ Harbor       │  │ Harbor (OCI) │  │ Bitbucket    │               │
│  │ Nexus        │  │ Nexus        │  │ Azure Repos  │               │
│  │ Private Reg. │  │ ChartMuseum  │  │ Self-hosted  │               │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘               │
│         │                 │                 │                       │
└─────────┼─────────────────┼─────────────────┼───────────────────────┘
          │  Pull/Push      │  Pull/Push      │  Clone/Push
          │  (Registry v2)  │  (OCI/HTTP)     │  (HTTPS/SSH)
          │                 │                 │
    ┌─────┴─────────────────┴─────────────────┴─────┐
    │                                               │
    │         Universal Airgapper                   │
    │                                               │
    │  Synchronizes container images, Helm charts,  │
    │  and Git repositories from source to          │
    │  destination registries/hosts.                │
    │                                               │
    │  Optionally runs external security scanner    │
    │  before promoting artifacts.                  │
    │                                               │
    └───────────────────┬───────────────────────────┘
                        │
          ┌─────────────┼─────────────────┐
          │             │                 │
    ┌─────┴─────┐ ┌─────┴─────┐ ┌─────────┴────────┐
    │ DevOps    │ │ CI/CD     │ │ Kubernetes       │
    │ Engineer  │ │ Pipelines │ │ Jobs/CronJobs    │
    │ (CLI)     │ │ (GH/GL)   │ │                  │
    └───────────┘ └───────────┘ └──────────────────┘
         Actors / Consumers
```

### Actors

| Actor                    | Description                                                        |
|--------------------------|--------------------------------------------------------------------|
| **DevOps Engineer**      | Runs the CLI locally to sync artifacts on demand.                  |
| **CI/CD Pipeline**       | GitHub Actions or GitLab CI invokes airgapper as a container step. |
| **Kubernetes Scheduler** | Runs airgapper as a Job or CronJob for periodic synchronization.   |

### External Systems

| System                    | Protocol                                    | Purpose                                                      |
|---------------------------|---------------------------------------------|--------------------------------------------------------------|
| **Container Registries**  | Docker Registry HTTP API v2                 | Pull and push OCI/Docker container images.                   |
| **Helm Chart Registries** | OCI Distribution / HTTP (legacy index.yaml) | Pull and push Helm charts.                                   |
| **Git Hosting Services**  | HTTPS / SSH                                 | Clone and push git repositories (branches, tags).            |
| **External Scanners**     | Shell command execution                     | Run security/compliance scans on artifacts before promotion. |

---

## Level 2: Container Diagram

The Container diagram zooms into Universal Airgapper to show its deployable units.

```
┌──────────────────────────────────────────────────────────────┐
│                   Universal Airgapper                        │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                    Go Binary                           │  │
│  │                 (airgapper CLI)                        │  │
│  │                                                        │  │
│  │  Single statically-linked binary.                      │  │
│  │  No runtime dependencies (CGO_ENABLED=0).              │  │
│  │  Reads YAML config files and credential files.         │  │
│  │  Executes sync operations against external registries. │  │
│  │  Optionally invokes external scanner commands.         │  │
│  │                                                        │  │
│  │  Entrypoint: cmd/airgapper/main.go                     │  │
│  │  All logic in: internal/                               │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │              Container Image                           │  │
│  │           (scratch / distroless)                       │  │
│  │                                                        │  │
│  │  Packages the Go binary for CI/CD and K8s deployment.  │  │
│  │  Published to GHCR on semver tags.                     │  │
│  │  Minimal attack surface — no shell, no OS packages.    │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────────┐  ┌──────────────────┐                  │
│  │  YAML Config     │  │ YAML Credentials │                  │
│  │  Files           │  │  Files           │                  │
│  │                  │  │                  │                  │
│  │ *.airgapper.yaml │  │Organized by type │                  │
│  │ Merged at load   │  │image/helm/git    │                  │
│  └──────────────────┘  └──────────────────┘                  │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### Deployment Contexts

| Context                | How it runs                                                     |
|------------------------|-----------------------------------------------------------------|
| **Local CLI**          | `airgapper sync --config ./configs/ --credentials ./creds/`     |
| **GitHub Actions**     | Reusable workflow runs the container image with mounted config  |
| **GitLab CI**          | Component template runs the container image with CI variables   |
| **Kubernetes Job**     | ConfigMap (config) + Secrets (credentials) mounted into the pod |
| **Kubernetes CronJob** | Same as Job, on a cron schedule                                 |

---

## Level 3: Component Diagram

The Component diagram shows the internal building blocks of the Go binary, organized by the hexagonal architecture.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Go Binary                                      │
│                                                                             │
│  ╔═══════════════════════════════════════════════════════════════════════╗  │
│  ║                     INBOUND ADAPTERS                                  ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────────────────────────────────────────────┐  ║  │
│  ║  │                    CLI Layer (cobra)                            │  ║  │
│  ║  │                                                                 │  ║  │
│  ║  │  internal/cli/root.go      — Root command, global flags         │  ║  │
│  ║  │  internal/cli/sync.go      — "sync" subcommand                  │  ║  │
│  ║  │  internal/cli/version.go   — "version" subcommand               │  ║  │
│  ║  │                                                                 │  ║  │
│  ║  │  Responsibilities:                                              │  ║  │
│  ║  │  • Parse CLI arguments and bind to viper                        │  ║  │
│  ║  │  • Initialize logging (slog JSON handler)                       │  ║  │
│  ║  │  • Load and validate configuration                              │  ║  │
│  ║  │  • Wire dependencies (composition root)                         │  ║  │
│  ║  │  • Invoke sync engine                                           │  ║  │
│  ║  │  • Map sync results to exit codes                               │  ║  │
│  ║  └─────────────────────────────────────────────────────────────────┘  ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────────────────────────────────────────────┐  ║  │
│  ║  │                Config Loader (viper)                            │  ║  │
│  ║  │                                                                 │  ║  │
│  ║  │  internal/config/loader.go    — Find and merge YAML files       │  ║  │
│  ║  │  internal/config/validate.go  — Schema validation               │  ║  │
│  ║  │  internal/config/types.go     — Typed config structs            │  ║  │
│  ║  │                                                                 │  ║  │
│  ║  │  Responsibilities:                                              │  ║  │
│  ║  │  • Discover *.airgapper.yaml files in config folder             │  ║  │
│  ║  │  • Merge multiple config files into unified config              │  ║  │
│  ║  │  • Unmarshal into typed Go structs                              │  ║  │
│  ║  │  • Validate all fields, fail fast on errors                     │  ║  │
│  ║  │  • Expand regex patterns for tags/versions/refs                 │  ║  │
│  ║  └─────────────────────────────────────────────────────────────────┘  ║  │
│  ╚═══════════════════════════════════════════════════════════════════════╝  │
│                                    │                                        │
│                                    ▼                                        │
│  ╔═══════════════════════════════════════════════════════════════════════╗  │
│  ║                         DOMAIN CORE                                   ║  │
│  ║                    (internal/domain/)                                 ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    ║  │
│  ║  │    Types        │  │   Interfaces    │  │   Errors            │    ║  │
│  ║  │                 │  │                 │  │                     │    ║  │
│  ║  │ Resource        │  │ Transporter     │  │ ErrNotFound         │    ║  │
│  ║  │ Endpoint        │  │ Scanner         │  │ ErrAuthFailed       │    ║  │
│  ║  │ Credential      │  │ CredentialStore │  │ ErrAlreadyExists    │    ║  │
│  ║  │ SyncResult      │  │                 │  │ ErrScanFailed       │    ║  │
│  ║  │ ScanResult      │  │                 │  │ ErrInvalidConfig    │    ║  │
│  ║  │ PushMode        │  │                 │  │                     │    ║  │
│  ║  └─────────────────┘  └─────────────────┘  └─────────────────────┘    ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────────────────────────────────────────────┐  ║  │
│  ║  │                  Sync Engine                                    │  ║  │
│  ║  │               (internal/sync/)                                  │  ║  │
│  ║  │                                                                 │  ║  │
│  ║  │  Responsibilities:                                              │  ║  │
│  ║  │  • Iterate over resources from config                           │  ║  │
│  ║  │  • Select transporter via factory (Strategy pattern)            │  ║  │
│  ║  │  • Run scanner before sync if configured                        │  ║  │
│  ║  │  • Invoke transporter.Sync() for each resource                  │  ║  │
│  ║  │  • Aggregate results (synced, skipped, failed)                  │  ║  │
│  ║  │  • Respect dry-run mode (skip all write operations)             │  ║  │
│  ║  │  • Concurrent sync via errgroup with context cancellation       │  ║  │
│  ║  └─────────────────────────────────────────────────────────────────┘  ║  │
│  ╚═══════════════════════════════════════════════════════════════════════╝  │
│                                    │                                        │
│                                    ▼                                        │
│  ╔═══════════════════════════════════════════════════════════════════════╗  │
│  ║                     OUTBOUND ADAPTERS                                 ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────┐  ┌─────────────────────────────────┐     ║  │
│  ║  │  Image Transporter      │  │  Credential Store               │     ║  │
│  ║  │  internal/transport/    │  │  internal/credentials/          │     ║  │
│  ║  │           image/        │  │                                 │     ║  │
│  ║  │                         │  │  • Load YAML credential files   │     ║  │
│  ║  │  • containers/image v5  │  │  • Resolve by host/name ref     │     ║  │
│  ║  │  • Pull via ImageSource │  │  • Support image/helm/git types │     ║  │
│  ║  │  • Push via ImageDest   │  │  • SSH key path resolution      │     ║  │
│  ║  │  • Multi-arch support   │  │  • Azure Repos workaround       │     ║  │
│  ║  │  • Manifest handling    │  │  • Never expose secrets in logs │     ║  │
│  ║  │  • Layer deduplication  │  │                                 │     ║  │
│  ║  └─────────────────────────┘  └─────────────────────────────────┘     ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────┐  ┌─────────────────────────────────┐     ║  │
│  ║  │  Helm Transporter       │  │  External Scanner               │     ║  │
│  ║  │  internal/transport/    │  │  internal/scanner/              │     ║  │
│  ║  │           helm/         │  │                                 │     ║  │
│  ║  │                         │  │  • Execute parameterized shell  │     ║  │
│  ║  │  • Helm v4 SDK          │  │    command with placeholders    │     ║  │
│  ║  │  • OCI registry support │  │  • Check return code            │     ║  │
│  ║  │  • Legacy HTTP repos    │  │  • Capture stdout/stderr        │     ║  │
│  ║  │  • Chart metadata       │  │  • Configurable pass/fail code  │     ║  │
│  ║  │  • Push to OCI / Nexus  │  │  • No tool-specific logic       │     ║  │
│  ║  └─────────────────────────┘  └─────────────────────────────────┘     ║  │
│  ║                                                                       ║  │
│  ║  ┌─────────────────────────┐                                          ║  │
│  ║  │  Git Transporter        │                                          ║  │
│  ║  │  internal/transport/    │                                          ║  │
│  ║  │           git/          │                                          ║  │
│  ║  │                         │                                          ║  │
│  ║  │  • go-git/go-git v5     │                                          ║  │
│  ║  │  • Clone specific refs  │                                          ║  │
│  ║  │  • HTTPS + SSH auth     │                                          ║  │
│  ║  │  • Push modes           │                                          ║  │
│  ║  │    (skip/push/force)    │                                          ║  │
│  ║  │  • Regex ref matching   │                                          ║  │
│  ║  └─────────────────────────┘                                          ║  │
│  ╚═══════════════════════════════════════════════════════════════════════╝  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component             | Package                     | Responsibility                                                                                                                                                           |
|-----------------------|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **CLI Layer**         | `internal/cli/`             | Parse arguments, wire dependencies, invoke engine, map results to exit codes.                                                                                            |
| **Config Loader**     | `internal/config/`          | Discover, merge, validate YAML config files. Unmarshal into typed structs.                                                                                               |
| **Domain Core**       | `internal/domain/`          | Define shared types (`Resource`, `Credential`, `SyncResult`), interfaces (`Transporter`, `Scanner`, `CredentialStore`), and sentinel errors. Zero external dependencies. |
| **Sync Engine**       | `internal/sync/`            | Orchestrate the sync workflow: iterate resources, select transporter, run scanner, execute sync, aggregate results.                                                      |
| **Image Transporter** | `internal/transport/image/` | Copy container images using `containers/image/v5`. Handle manifests, layers, multi-arch, auth tokens.                                                                    |
| **Helm Transporter**  | `internal/transport/helm/`  | Copy Helm charts using the Helm v4 SDK. Handle OCI and legacy HTTP registries.                                                                                           |
| **Git Transporter**   | `internal/transport/git/`   | Clone and push git repos using `go-git/go-git/v5`. Handle HTTPS/SSH auth, regex ref matching.                                                                            |
| **Credential Store**  | `internal/credentials/`     | Load credential YAML files, resolve credentials by host/name reference, support multiple credential types.                                                               |
| **External Scanner**  | `internal/scanner/`         | Execute parameterized shell commands, check return codes, capture output. Generic — no tool-specific logic.                                                              |
| **Logging**           | `internal/logging/`         | Configure `slog` with JSON handler, set log level from flags, provide contextual logger creation.                                                                        |

---

## Level 4: Code Diagram

### Domain Interfaces (Ports)

These interfaces define the contracts between the domain core and the adapters.
They are the "ports" in hexagonal architecture.

```go
// internal/domain/transporter.go

// Transporter defines the contract for syncing artifacts between endpoints.
// Each artifact type (image, helm, git) provides its own implementation.
// This is the primary outbound port of the hexagonal architecture.
type Transporter interface {
    // Type returns the resource type this transporter handles ("image", "helm", "git").
    Type() string

    // Sync copies a single versioned artifact from source to destination.
    // It respects PushMode (skip, force, overwrite) and dry-run settings.
    // Returns a SyncResult with per-version outcomes.
    Sync(ctx context.Context, resource Resource, opts SyncOptions) (*SyncResult, error)

    // Exists checks whether a specific version of a resource exists at the destination.
    Exists(ctx context.Context, endpoint Endpoint, version string, creds *Credential) (bool, error)
}
```

```go
// internal/domain/scanner.go

// Scanner defines the contract for running security/compliance checks on artifacts.
// The implementation executes an external command and interprets the result.
type Scanner interface {
    // Name returns the scanner identifier (matches config reference).
    Name() string

    // Scan runs the configured command against the given artifact reference.
    // Returns a ScanResult indicating pass/fail and captured output.
    Scan(ctx context.Context, artifact ArtifactRef) (*ScanResult, error)
}
```

```go
// internal/domain/credential_store.go

// CredentialStore loads and resolves credentials for registry/host access.
type CredentialStore interface {
    // Resolve finds credentials matching the given host and credential type.
    Resolve(host string, credType CredentialType) (*Credential, error)

    // ResolveByRef finds credentials by their reference name.
    ResolveByRef(ref string) (*Credential, error)
}
```

### Core Domain Types

```go
// internal/domain/types.go

// Resource represents a single artifact to synchronize, parsed from config.
type Resource struct {
    Type                   ResourceType  // "image", "helm", "git"
    Source                 Endpoint      // Source registry/repo
    Destination            Endpoint      // Destination registry/repo
    Versions               []string      // Tags, versions, or refs (may contain regex)
    PushMode               PushMode      // skip, force, overwrite
    ScannerRef             string        // Optional scanner name reference
    SourceCredentialsRef   string        // Optional source credentials reference
    TargetCredentialsRef   string        // Optional target credentials reference
}

// Endpoint represents a registry or repository location.
type Endpoint struct {
    Registry   string // Registry hostname (e.g., "registry-1.docker.io")
    Repository string // Repository path (e.g., "library/ubuntu")
    // For git: the full URL (HTTPS or SSH)
}

// SyncOptions carries runtime settings that apply to all sync operations.
type SyncOptions struct {
    DryRun         bool
    Credentials    CredentialStore
    Logger         *slog.Logger
}

// SyncResult reports the outcome of syncing a single resource.
type SyncResult struct {
    Resource   Resource
    Synced     []VersionResult // Successfully synced versions
    Skipped    []VersionResult // Already present, skipped
    Failed     []VersionResult // Failed versions with errors
}

// VersionResult represents the outcome of syncing a single version/tag/ref.
type VersionResult struct {
    Version string
    Status  SyncStatus // synced, skipped, failed
    Error   error      // nil unless failed
    Message string     // Human-readable status message
}

// ArtifactRef identifies a specific artifact for scanning.
type ArtifactRef struct {
    Type       ResourceType
    Registry   string
    Repository string
    Version    string
}

// ScanResult represents the outcome of a security scan.
type ScanResult struct {
    Passed  bool
    Output  string // Captured stdout
    Error   string // Captured stderr
    Code    int    // Exit code of the scanner command
}

// Credential holds authentication data for a registry or host.
type Credential struct {
    Name       string
    Type       CredentialType // image, helm, git
    Username   string
    Password   string
    SSHKeyPath string // For git SSH authentication
}
```

### Enumerations

```go
// internal/domain/enums.go

type ResourceType string
const (
    ResourceTypeImage ResourceType = "image"
    ResourceTypeHelm  ResourceType = "helm"
    ResourceTypeGit   ResourceType = "git"
)

type PushMode string
const (
    PushModeSkip      PushMode = "skip"
    PushModeForce     PushMode = "force"
    PushModeOverwrite PushMode = "overwrite"
)

type SyncStatus string
const (
    SyncStatusSynced  SyncStatus = "synced"
    SyncStatusSkipped SyncStatus = "skipped"
    SyncStatusFailed  SyncStatus = "failed"
)

type CredentialType string
const (
    CredentialTypeImage CredentialType = "image"
    CredentialTypeHelm  CredentialType = "helm"
    CredentialTypeGit   CredentialType = "git"
)
```

### Sync Engine (Orchestrator)

```go
// internal/sync/engine.go

// Engine orchestrates the sync workflow across all resources.
type Engine struct {
    transporters map[ResourceType]Transporter
    scanners     map[string]Scanner
    logger       *slog.Logger
}

// NewEngine creates a sync engine with the given transporters and scanners.
func NewEngine(
    transporters map[ResourceType]Transporter,
    scanners map[string]Scanner,
    logger *slog.Logger,
) *Engine

// Run processes all resources, returning aggregated results.
// It uses errgroup for concurrent sync with context cancellation.
func (e *Engine) Run(ctx context.Context, resources []Resource, opts SyncOptions) ([]SyncResult, error)
```

### Transport Factory

```go
// internal/transport/factory.go

// Factory creates Transporter instances based on resource type.
type Factory struct {
    registry map[ResourceType]NewTransporterFunc
}

type NewTransporterFunc func(logger *slog.Logger) (Transporter, error)

// NewFactory creates a factory with all registered transporter constructors.
func NewFactory(logger *slog.Logger) *Factory

// ForType returns the transporter for the given resource type.
func (f *Factory) ForType(rt ResourceType) (Transporter, error)
```

### Dependency Flow

```
main.go
  │
  ├─► config.Load()           → Config struct
  ├─► credentials.NewStore()  → CredentialStore (interface)
  ├─► transport.NewFactory()  → map[ResourceType]Transporter
  ├─► scanner.New()           → map[string]Scanner
  ├─► sync.NewEngine()        → Engine (uses interfaces only)
  └─► cli.NewRootCmd()        → cobra.Command
        │
        └─► engine.Run()      → []SyncResult
              │
              ├─► transporter.Sync()  (via interface)
              ├─► scanner.Scan()      (via interface)
              └─► creds.Resolve()     (via interface)
```
