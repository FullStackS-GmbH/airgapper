// Package domain defines the core business types, interfaces, and errors for
// Universal Airgapper. This package has zero external dependencies — it imports
// only the Go standard library. All other packages depend on domain; domain
// depends on nothing.
package domain

// ResourceType identifies the kind of artifact being synchronized.
type ResourceType string

const (
	// ResourceTypeImage represents OCI / Docker container images.
	ResourceTypeImage ResourceType = "image"
	// ResourceTypeHelm represents Helm charts (OCI or legacy HTTP).
	ResourceTypeHelm ResourceType = "helm"
	// ResourceTypeGit represents Git repositories.
	ResourceTypeGit ResourceType = "git"
)

// String returns the string representation of the ResourceType.
func (rt ResourceType) String() string { return string(rt) }

// Valid returns true if the ResourceType is one of the known types.
func (rt ResourceType) Valid() bool {
	switch rt {
	case ResourceTypeImage, ResourceTypeHelm, ResourceTypeGit:
		return true
	}
	return false
}

// PushMode controls how the tool handles artifacts that already exist at the
// destination.
type PushMode string

const (
	// PushModeSkip skips the artifact if it already exists at the destination.
	PushModeSkip PushMode = "skip"
	// PushModeForce overwrites the artifact at the destination unconditionally.
	PushModeForce PushMode = "force"
	// PushModeOverwrite is an alias for force, used by Helm charts.
	PushModeOverwrite PushMode = "overwrite"
)

// String returns the string representation of the PushMode.
func (pm PushMode) String() string { return string(pm) }

// Valid returns true if the PushMode is one of the known modes.
func (pm PushMode) Valid() bool {
	switch pm {
	case PushModeSkip, PushModeForce, PushModeOverwrite:
		return true
	}
	return false
}

// SyncStatus describes the outcome of syncing a single version.
type SyncStatus string

const (
	// SyncStatusSynced indicates the version was successfully copied.
	SyncStatusSynced SyncStatus = "synced"
	// SyncStatusSkipped indicates the version was skipped (already exists or dry-run).
	SyncStatusSkipped SyncStatus = "skipped"
	// SyncStatusFailed indicates the version sync failed.
	SyncStatusFailed SyncStatus = "failed"
)

// String returns the string representation of the SyncStatus.
func (ss SyncStatus) String() string { return string(ss) }

// CredentialType identifies the kind of service a credential is used for.
type CredentialType string

const (
	// CredentialTypeImage is for container image registries.
	CredentialTypeImage CredentialType = "image"
	// CredentialTypeHelm is for Helm chart registries.
	CredentialTypeHelm CredentialType = "helm"
	// CredentialTypeGit is for Git hosting services.
	CredentialTypeGit CredentialType = "git"
)

// String returns the string representation of the CredentialType.
func (ct CredentialType) String() string { return string(ct) }
