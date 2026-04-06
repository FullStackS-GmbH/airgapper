package domain

import "context"

// Transporter defines the contract for syncing artifacts between endpoints.
// Each artifact type (image, helm, git) provides its own implementation.
// This is the primary outbound port of the hexagonal architecture.
//
// Implementations must be safe for concurrent use from multiple goroutines.
type Transporter interface {
	// Type returns the resource type this transporter handles
	// (e.g. ResourceTypeImage, ResourceTypeHelm, ResourceTypeGit).
	Type() ResourceType

	// Sync copies versioned artifacts from source to destination as described
	// by the resource. It respects PushMode and the DryRun flag in SyncOptions.
	// Sync must be idempotent: calling it twice with identical input produces
	// the same result.
	Sync(ctx context.Context, resource Resource, opts SyncOptions) (*SyncResult, error)

	// Exists checks whether a specific version of an artifact exists at the
	// given endpoint. It must not modify any state.
	Exists(ctx context.Context, endpoint Endpoint, version string, creds *Credential) (bool, error)

	// ListVersions returns all available versions (tags, chart versions, or
	// git refs) at the given endpoint. The returned slice is unordered; the
	// caller handles filtering and regex matching.
	ListVersions(ctx context.Context, endpoint Endpoint, creds *Credential) ([]string, error)
}
