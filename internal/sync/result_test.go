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
			want: syncpkg.Summary{
				ByType: make(map[domain.ResourceType]syncpkg.ResourceTypeSummary),
			},
		},
		{
			name: "single result all synced",
			results: []domain.SyncResult{
				{
					Resource: domain.Resource{Type: domain.ResourceTypeImage},
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
				ByType: map[domain.ResourceType]syncpkg.ResourceTypeSummary{
					domain.ResourceTypeImage: {
						Type:     domain.ResourceTypeImage,
						Versions: 2,
						Synced:   2,
					},
				},
			},
		},
		{
			name: "mixed results",
			results: []domain.SyncResult{
				{
					Resource: domain.Resource{Type: domain.ResourceTypeImage},
					Synced:   []domain.VersionResult{{Version: "v1.0.0"}},
					Skipped:  []domain.VersionResult{{Version: "v2.0.0"}},
					Failed:   []domain.VersionResult{{Version: "v3.0.0"}},
				},
				{
					Resource: domain.Resource{Type: domain.ResourceTypeHelm},
					Synced:   []domain.VersionResult{{Version: "v4.0.0"}},
				},
			},
			want: syncpkg.Summary{
				TotalResources: 2,
				TotalVersions:  4,
				Synced:         2,
				Skipped:        1,
				Failed:         1,
				ByType: map[domain.ResourceType]syncpkg.ResourceTypeSummary{
					domain.ResourceTypeImage: {
						Type:     domain.ResourceTypeImage,
						Versions: 3,
						Synced:   1,
						Skipped:  1,
						Failed:   1,
					},
					domain.ResourceTypeHelm: {
						Type:     domain.ResourceTypeHelm,
						Versions: 1,
						Synced:   1,
					},
				},
			},
		},
		{
			name: "all failed",
			results: []domain.SyncResult{
				{
					Resource: domain.Resource{Type: domain.ResourceTypeGit},
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
				ByType: map[domain.ResourceType]syncpkg.ResourceTypeSummary{
					domain.ResourceTypeGit: {
						Type:     domain.ResourceTypeGit,
						Versions: 2,
						Failed:   2,
					},
				},
			},
		},
		{
			name: "with operations",
			results: []domain.SyncResult{
				{
					Resource: domain.Resource{Type: domain.ResourceTypeImage},
					Synced:   []domain.VersionResult{{Version: "v1.0.0"}},
					Operations: []domain.OperationRecord{
						{Operation: domain.OpRead},
						{Operation: domain.OpPull},
						{Operation: domain.OpPush},
					},
				},
			},
			want: syncpkg.Summary{
				TotalResources: 1,
				TotalVersions:  1,
				Synced:         1,
				Operations:     syncpkg.OperationCounts{Read: 1, Pull: 1, Push: 1},
				ByType: map[domain.ResourceType]syncpkg.ResourceTypeSummary{
					domain.ResourceTypeImage: {
						Type:       domain.ResourceTypeImage,
						Versions:   1,
						Synced:     1,
						Operations: syncpkg.OperationCounts{Read: 1, Pull: 1, Push: 1},
					},
				},
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

func TestFormatSummary(t *testing.T) {
	t.Parallel()

	s := syncpkg.Summary{
		TotalResources: 3,
		TotalVersions:  10,
		Synced:         7,
		Skipped:        2,
		Failed:         1,
		Operations:     syncpkg.OperationCounts{Read: 3, Pull: 7, Push: 7, Skip: 2, Fail: 1},
		ByType: map[domain.ResourceType]syncpkg.ResourceTypeSummary{
			domain.ResourceTypeImage: {Type: domain.ResourceTypeImage, Versions: 5, Synced: 4, Skipped: 1},
			domain.ResourceTypeHelm:  {Type: domain.ResourceTypeHelm, Versions: 3, Synced: 2, Failed: 1},
			domain.ResourceTypeGit:   {Type: domain.ResourceTypeGit, Versions: 2, Synced: 1, Skipped: 1},
		},
	}

	got := syncpkg.FormatSummary(s)
	assert.Contains(t, got, "Sync Summary")
	assert.Contains(t, got, "Resources: 3")
	assert.Contains(t, got, "Synced: 7")
	assert.Contains(t, got, "read=3")
	assert.Contains(t, got, "pull=7")
	assert.Contains(t, got, "image: 5 versions")
	assert.Contains(t, got, "helm: 3 versions")
	assert.Contains(t, got, "git: 2 versions")
}
