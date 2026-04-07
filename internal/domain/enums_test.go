package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

func TestResourceType_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rt   domain.ResourceType
		want bool
	}{
		{"image is valid", domain.ResourceTypeImage, true},
		{"helm is valid", domain.ResourceTypeHelm, true},
		{"git is valid", domain.ResourceTypeGit, true},
		{"empty is invalid", domain.ResourceType(""), false},
		{"unknown is invalid", domain.ResourceType("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.rt.Valid())
		})
	}
}

func TestResourceType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rt   domain.ResourceType
		want string
	}{
		{"image", domain.ResourceTypeImage, "image"},
		{"helm", domain.ResourceTypeHelm, "helm"},
		{"git", domain.ResourceTypeGit, "git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.rt.String())
		})
	}
}

func TestPushMode_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pm   domain.PushMode
		want bool
	}{
		{"skip is valid", domain.PushModeSkip, true},
		{"force is valid", domain.PushModeForce, true},
		{"overwrite is valid", domain.PushModeOverwrite, true},
		{"empty is invalid", domain.PushMode(""), false},
		{"unknown is invalid", domain.PushMode("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.pm.Valid())
		})
	}
}

func TestPushMode_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pm   domain.PushMode
		want string
	}{
		{"skip", domain.PushModeSkip, "skip"},
		{"force", domain.PushModeForce, "force"},
		{"overwrite", domain.PushModeOverwrite, "overwrite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.pm.String())
		})
	}
}

func TestSyncStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ss   domain.SyncStatus
		want string
	}{
		{"synced", domain.SyncStatusSynced, "synced"},
		{"skipped", domain.SyncStatusSkipped, "skipped"},
		{"failed", domain.SyncStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.ss.String())
		})
	}
}

func TestCredentialType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ct   domain.CredentialType
		want string
	}{
		{"image", domain.CredentialTypeImage, "image"},
		{"helm", domain.CredentialTypeHelm, "helm"},
		{"git", domain.CredentialTypeGit, "git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.ct.String())
		})
	}
}
