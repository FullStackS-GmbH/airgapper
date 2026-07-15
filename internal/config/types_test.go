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
