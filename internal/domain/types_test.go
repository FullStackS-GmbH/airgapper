package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestEndpoint_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint domain.Endpoint
		want     string
	}{
		{
			name:     "registry and repository",
			endpoint: domain.Endpoint{Registry: "registry.example.com", Repository: "repo/image"},
			want:     "registry.example.com/repo/image",
		},
		{
			name:     "repository only",
			endpoint: domain.Endpoint{Repository: "library/ubuntu"},
			want:     "library/ubuntu",
		},
		{
			name:     "empty endpoint",
			endpoint: domain.Endpoint{},
			want:     "",
		},
		{
			name:     "git URL as repository",
			endpoint: domain.Endpoint{Repository: "https://github.com/org/repo.git"},
			want:     "https://github.com/org/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.endpoint.String())
		})
	}
}

func TestSyncResult_TotalCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sr   domain.SyncResult
		want int
	}{
		{
			name: "all categories",
			sr: domain.SyncResult{
				Synced:  []domain.VersionResult{{Version: "v1"}, {Version: "v2"}},
				Skipped: []domain.VersionResult{{Version: "v3"}},
				Failed:  []domain.VersionResult{{Version: "v4"}},
			},
			want: 4,
		},
		{
			name: "empty result",
			sr:   domain.SyncResult{},
			want: 0,
		},
		{
			name: "synced only",
			sr: domain.SyncResult{
				Synced: []domain.VersionResult{{Version: "v1"}},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.sr.TotalCount())
		})
	}
}

func TestSyncResult_HasFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sr   domain.SyncResult
		want bool
	}{
		{
			name: "has failures",
			sr: domain.SyncResult{
				Failed: []domain.VersionResult{{Version: "v1"}},
			},
			want: true,
		},
		{
			name: "no failures",
			sr: domain.SyncResult{
				Synced: []domain.VersionResult{{Version: "v1"}},
			},
			want: false,
		},
		{
			name: "empty result",
			sr:   domain.SyncResult{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.sr.HasFailures())
		})
	}
}

func TestArtifactRef_FullRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ar   domain.ArtifactRef
		want string
	}{
		{
			name: "with registry",
			ar: domain.ArtifactRef{
				Registry:   "registry.example.com",
				Repository: "repo/image",
				Version:    "v1.0.0",
			},
			want: "registry.example.com/repo/image:v1.0.0",
		},
		{
			name: "without registry",
			ar: domain.ArtifactRef{
				Repository: "library/ubuntu",
				Version:    "latest",
			},
			want: "library/ubuntu:latest",
		},
		{
			name: "empty version",
			ar: domain.ArtifactRef{
				Registry:   "ghcr.io",
				Repository: "org/app",
				Version:    "",
			},
			want: "ghcr.io/org/app:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.ar.FullRef())
		})
	}
}
