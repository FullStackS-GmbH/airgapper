package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestResourceConfigToResource_NormalizesHelmEndpointSlashes(t *testing.T) {
	t.Parallel()

	rc := config.ResourceConfig{
		Type:                "helm",
		SourceRegistry:      "charts.rancher.io/",
		SourceChart:         "/neuvector-crd/",
		DestinationRegistry: "localhost:5050/",
		DestinationRepo:     "/platform-charts/",
		DestinationChart:    "/suse-private-registry/",
		Versions:            []string{"108.0.1+up2.8.10"},
	}

	got := rc.ToResource()

	assert.Equal(t, domain.Endpoint{Registry: "charts.rancher.io", Repository: "neuvector-crd"}, got.Source)
	assert.Equal(t, domain.Endpoint{Registry: "localhost:5050", Repository: "platform-charts"}, got.Destination)
	assert.Equal(t, "suse-private-registry", got.DestinationChart)
}

func TestResourceConfigToResource_AppliesTLSOptionsForImageAndHelm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rc   config.ResourceConfig
	}{
		{
			name: "image",
			rc: config.ResourceConfig{
				Type:        "image",
				Source:      "registry.example.com/team/app",
				Destination: "internal.example.com/mirror/app",
				Tags:        []string{"v1"},
			},
		},
		{
			name: "helm",
			rc: config.ResourceConfig{
				Type:                "helm",
				SourceRegistry:      "registry.example.com",
				SourceChart:         "team/app",
				DestinationRegistry: "internal.example.com",
				DestinationRepo:     "mirror",
				Versions:            []string{"1.0.0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.rc.SourceInsecure = true
			tt.rc.DestinationCACert = "/etc/airgapper/certs.d/internal"

			got := tt.rc.ToResource()

			assert.True(t, got.Source.Insecure)
			assert.False(t, got.Destination.Insecure)
			assert.Equal(t, "/etc/airgapper/certs.d/internal", got.Destination.CACertPath)
			assert.Empty(t, got.Source.CACertPath)
		})
	}
}

func TestResourceConfigToResource_IgnoresTLSOptionsForGit(t *testing.T) {
	t.Parallel()

	rc := config.ResourceConfig{
		Type:              "git",
		SourceRepo:        "git@github.com:org/project.git",
		DestinationRepo:   "git@internal.example.com:mirror/project.git",
		Refs:              []string{"main"},
		SourceInsecure:    true,
		DestinationCACert: "/etc/airgapper/certs.d/internal",
	}

	got := rc.ToResource()

	assert.False(t, got.Source.Insecure)
	assert.False(t, got.Destination.Insecure)
	assert.Empty(t, got.Source.CACertPath)
	assert.Empty(t, got.Destination.CACertPath)
}

func TestResourceConfigToResource_AppliesPolicyPathForImageOnly(t *testing.T) {
	t.Parallel()

	imageRC := config.ResourceConfig{
		Type:        "image",
		Source:      "registry.example.com/team/app",
		Destination: "internal.example.com/mirror/app",
		Tags:        []string{"v1"},
		PolicyPath:  "/etc/airgapper/policy.json",
	}
	assert.Equal(t, "/etc/airgapper/policy.json", imageRC.ToResource().PolicyPath)

	helmRC := config.ResourceConfig{
		Type:                "helm",
		SourceRegistry:      "registry.example.com",
		SourceChart:         "team/app",
		DestinationRegistry: "internal.example.com",
		DestinationRepo:     "mirror",
		Versions:            []string{"1.0.0"},
		PolicyPath:          "/etc/airgapper/policy.json",
	}
	assert.Empty(t, helmRC.ToResource().PolicyPath)

	gitRC := config.ResourceConfig{
		Type:            "git",
		SourceRepo:      "git@github.com:org/project.git",
		DestinationRepo: "git@internal.example.com:mirror/project.git",
		Refs:            []string{"main"},
		PolicyPath:      "/etc/airgapper/policy.json",
	}
	assert.Empty(t, gitRC.ToResource().PolicyPath)
}

func TestResourceConfigToResource_ImagePlatforms(t *testing.T) {
	t.Parallel()

	amd64 := domain.Platform{OS: "linux", Architecture: "amd64"}
	arm64 := domain.Platform{OS: "linux", Architecture: "arm64"}
	for _, tt := range []struct {
		name      string
		platforms []string
		want      []domain.Platform
	}{
		{name: "defaults to linux/amd64", want: []domain.Platform{amd64}},
		{name: "all copies every platform", platforms: []string{"all"}, want: nil},
		{name: "all is case-insensitive", platforms: []string{" ALL "}, want: nil},
		{name: "explicit list is normalized and deduplicated", platforms: []string{"linux/amd64", " Linux/ARM64 ", "linux/amd64"}, want: []domain.Platform{amd64, arm64}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rc := config.ResourceConfig{Type: "image", Source: "a.io/x", Destination: "b.io/x", Tags: []string{"v1"}, Platforms: tt.platforms}
			assert.Equal(t, tt.want, rc.ToResource().Platforms)
		})
	}

	helm := config.ResourceConfig{Type: "helm", SourceRegistry: "a.io", SourceChart: "c", DestinationRegistry: "b.io", DestinationRepo: "r", Versions: []string{"1"}}
	assert.Nil(t, helm.ToResource().Platforms, "platforms apply to image resources only")
}
