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
