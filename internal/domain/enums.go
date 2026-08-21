// Package domain defines the core business types, interfaces, and errors for
// Universal Airgapper. This package has zero external dependencies — it imports
// only the Go standard library. All other packages depend on domain; domain
// depends on nothing.
package domain

import "strings"

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

// PushMode controls how the tool handles artifacts that already exist at the
// destination.
type PushMode string

const (
	// PushModeSkip skips the artifact if it already exists at the destination.
	PushModeSkip PushMode = "skip"
	// PushModeForce overwrites the artifact at the destination unconditionally.
	// The configuration value "overwrite" is accepted as an alias and is
	// normalized to force by ResourceConfig.ToResource.
	PushModeForce PushMode = "force"
)

// ParsePushMode converts a configuration string into a PushMode. Matching is
// case-insensitive and surrounding whitespace is ignored. An empty string
// yields PushModeSkip. The legacy value "overwrite" is an alias for force.
// The boolean reports whether the value was recognised.
func ParsePushMode(s string) (PushMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(PushModeSkip):
		return PushModeSkip, true
	case string(PushModeForce), "overwrite":
		return PushModeForce, true
	}
	return "", false
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
