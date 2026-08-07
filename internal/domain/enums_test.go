package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

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

func TestParsePushMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		want  domain.PushMode
		wantK bool
	}{
		{"empty defaults to skip", "", domain.PushModeSkip, true},
		{"skip", "skip", domain.PushModeSkip, true},
		{"force", "force", domain.PushModeForce, true},
		{"overwrite is an alias for force", "overwrite", domain.PushModeForce, true},
		{"case insensitive", "FORCE", domain.PushModeForce, true},
		{"whitespace trimmed", "  skip  ", domain.PushModeSkip, true},
		{"unknown is rejected", "clobber", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ParsePushMode(tt.in)
			assert.Equal(t, tt.wantK, ok)
			assert.Equal(t, tt.want, got)
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
