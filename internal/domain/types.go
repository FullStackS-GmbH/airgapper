package domain

import "log/slog"

// Resource represents a single artifact to synchronize, parsed from the
// configuration file. It is the primary input to the sync engine.
type Resource struct {
	// Type identifies the artifact kind (image, helm, git).
	Type ResourceType

	// Source is the origin endpoint from which the artifact is pulled.
	Source Endpoint

	// Destination is the target endpoint to which the artifact is pushed.
	Destination Endpoint

	// Versions lists the tags, chart versions, or git refs to sync.
	// Entries may be literal values or regex patterns.
	Versions []string

	// PushMode controls behaviour when the artifact already exists at the
	// destination: skip, force, or overwrite.
	PushMode PushMode

	// ScannerRef is the optional name of a scanner to run before syncing.
	ScannerRef string

	// SourceCredentialsRef is the optional name of the credential entry used
	// to authenticate against the source.
	SourceCredentialsRef string

	// TargetCredentialsRef is the optional name of the credential entry used
	// to authenticate against the destination.
	TargetCredentialsRef string
}

// Endpoint represents a registry or repository location.
type Endpoint struct {
	// Registry is the hostname of the registry (e.g. "registry-1.docker.io").
	// For git resources this may be empty; the full URL is stored in Repository.
	Registry string

	// Repository is the path within the registry (e.g. "library/ubuntu") or,
	// for git resources, the full clone URL (HTTPS or SSH).
	Repository string
}

// String returns a human-readable representation of the endpoint.
func (e Endpoint) String() string {
	if e.Registry == "" {
		return e.Repository
	}
	return e.Registry + "/" + e.Repository
}

// SyncOptions carries runtime settings that apply to all sync operations.
type SyncOptions struct {
	// DryRun disables all write/push operations when set to true.
	DryRun bool

	// Credentials provides access to the credential store for resolving
	// authentication data.
	Credentials CredentialStore

	// Logger is the structured logger for the current sync run.
	Logger *slog.Logger
}

// SyncResult reports the outcome of syncing a single resource across all its
// versions.
type SyncResult struct {
	// Resource is the resource that was synced.
	Resource Resource

	// Synced contains versions that were successfully copied.
	Synced []VersionResult

	// Skipped contains versions that were skipped (already present or dry-run).
	Skipped []VersionResult

	// Failed contains versions that failed to sync.
	Failed []VersionResult
}

// TotalCount returns the total number of versions processed.
func (sr *SyncResult) TotalCount() int {
	return len(sr.Synced) + len(sr.Skipped) + len(sr.Failed)
}

// HasFailures returns true if any version failed to sync.
func (sr *SyncResult) HasFailures() bool {
	return len(sr.Failed) > 0
}

// VersionResult represents the outcome of syncing a single version/tag/ref.
type VersionResult struct {
	// Version is the tag, chart version, or git ref that was processed.
	Version string

	// Status is the outcome: synced, skipped, or failed.
	Status SyncStatus

	// Error is non-nil only when Status is SyncStatusFailed.
	Error error

	// Message is a human-readable explanation of the outcome.
	Message string
}

// ArtifactRef identifies a specific artifact for scanning. It provides enough
// information for a scanner command to construct its target reference.
type ArtifactRef struct {
	// Type is the resource type (image, helm, git).
	Type ResourceType

	// Registry is the hostname of the registry.
	Registry string

	// Repository is the path within the registry.
	Repository string

	// Version is the specific tag, chart version, or git ref.
	Version string
}

// FullRef returns the complete artifact reference string
// (e.g. "registry.example.com/repo/image:v1.0.0").
func (ar ArtifactRef) FullRef() string {
	if ar.Registry == "" {
		return ar.Repository + ":" + ar.Version
	}
	return ar.Registry + "/" + ar.Repository + ":" + ar.Version
}

// ScanResult represents the outcome of a security scan executed by an external
// scanner command.
type ScanResult struct {
	// Passed is true when the scanner exit code matched the configured success code.
	Passed bool

	// Output is the captured stdout of the scanner command.
	Output string

	// ErrorOutput is the captured stderr of the scanner command.
	ErrorOutput string

	// ExitCode is the actual exit code returned by the scanner process.
	ExitCode int
}

// Credential holds authentication data for a registry, repository, or service.
type Credential struct {
	// Name is the credential identifier, used for reference lookups.
	Name string

	// Type identifies the service this credential is for (image, helm, git).
	Type CredentialType

	// Username is the authentication username. May be empty for SSH-only auth.
	Username string

	// Password is the authentication password or token. May be empty for
	// SSH-only auth. For Azure Repos HTTPS, this is an environment variable
	// name containing the Authorization header.
	Password string

	// SSHKeyPath is the filesystem path to an SSH private key file. Used for
	// git SSH authentication. Empty when not applicable.
	SSHKeyPath string
}
