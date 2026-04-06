package sync_test

import (
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	syncpkg "github.com/fullstacks-gmbh/universal-airgapper/internal/sync"
	"github.com/stretchr/testify/assert"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []domain.SyncResult
		want    syncpkg.Summary
	}{
		{
			name:    "empty results",
			results: nil,
			want:    syncpkg.Summary{},
		},
		{
			name: "single result all synced",
			results: []domain.SyncResult{
				{
					Synced: []domain.VersionResult{
						{Version: "v1.0.0", Status: domain.SyncStatusSynced},
						{Version: "v2.0.0", Status: domain.SyncStatusSynced},
					},
				},
			},
			want: syncpkg.Summary{
				TotalResources: 1,
				TotalVersions:  2,
				Synced:         2,
			},
		},
		{
			name: "mixed results",
			results: []domain.SyncResult{
				{
					Synced:  []domain.VersionResult{{Version: "v1.0.0"}},
					Skipped: []domain.VersionResult{{Version: "v2.0.0"}},
					Failed:  []domain.VersionResult{{Version: "v3.0.0"}},
				},
				{
					Synced: []domain.VersionResult{{Version: "v4.0.0"}},
				},
			},
			want: syncpkg.Summary{
				TotalResources: 2,
				TotalVersions:  4,
				Synced:         2,
				Skipped:        1,
				Failed:         1,
			},
		},
		{
			name: "all failed",
			results: []domain.SyncResult{
				{
					Failed: []domain.VersionResult{
						{Version: "v1.0.0"},
						{Version: "v2.0.0"},
					},
				},
			},
			want: syncpkg.Summary{
				TotalResources: 1,
				TotalVersions:  2,
				Failed:         2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := syncpkg.Summarize(tt.results)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType domain.ResourceType
		source       string
		destination  string
		vr           domain.VersionResult
		wantContains []string
	}{
		{
			name:         "synced with no message",
			resourceType: domain.ResourceTypeImage,
			source:       "docker.io/library/ubuntu",
			destination:  "internal.io/library/ubuntu",
			vr:           domain.VersionResult{Version: "v1.0.0", Status: domain.SyncStatusSynced},
			wantContains: []string{"[image]", "docker.io/library/ubuntu:v1.0.0", "internal.io/library/ubuntu:v1.0.0", "SYNCED"},
		},
		{
			name:         "skipped with message",
			resourceType: domain.ResourceTypeHelm,
			source:       "charts.io/mychart",
			destination:  "internal.io/mychart",
			vr:           domain.VersionResult{Version: "1.0.0", Status: domain.SyncStatusSkipped, Message: "already exists"},
			wantContains: []string{"[helm]", "SKIPPED", "(already exists)"},
		},
		{
			name:         "failed",
			resourceType: domain.ResourceTypeGit,
			source:       "github.com/org/repo",
			destination:  "internal.git/org/repo",
			vr:           domain.VersionResult{Version: "main", Status: domain.SyncStatusFailed, Message: "auth error"},
			wantContains: []string{"[git]", "FAILED", "(auth error)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := syncpkg.FormatResult(tt.resourceType, tt.source, tt.destination, tt.vr)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
		})
	}
}
