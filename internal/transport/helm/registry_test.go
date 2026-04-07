package helm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/transport/helm"
)

func TestIsOCIRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"docker.io", "docker.io", true},
		{"registry-1.docker.io", "registry-1.docker.io", true},
		{"ghcr.io", "ghcr.io", true},
		{"quay.io", "quay.io", true},
		{"harbor.io", "harbor.io", true},
		{"registry.k8s.io", "registry.k8s.io", true},
		{"oci:// prefix", "oci://my-registry.example.com", true},
		{"Azure ACR suffix", "myregistry.azurecr.io", true},
		{"Google Artifact Registry", "us-docker.pkg.dev", true},
		{"Google GCR subdomain", "us.gcr.io", true},
		{"AWS ECR", "123456789.dkr.ecr.us-east-1.ecr.aws", true},
		{"public ECR", "public.ecr.aws", true},
		{"legacy HTTP repo", "https://charts.helm.sh/stable", false},
		{"unknown registry", "my-private-registry.local", false},
		{"with port", "ghcr.io:443", true},
		{"with path", "ghcr.io/myorg", true},
		{"with scheme", "https://ghcr.io/myorg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, helm.IsOCIRegistry(tt.url))
		})
	}
}

func TestNormalizeOCIRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		chart    string
		version  string
		want     string
	}{
		{
			name:     "basic ref with version",
			registry: "ghcr.io",
			chart:    "mychart",
			version:  "1.0.0",
			want:     "oci://ghcr.io/mychart:1.0.0",
		},
		{
			name:     "no version",
			registry: "ghcr.io",
			chart:    "mychart",
			version:  "",
			want:     "oci://ghcr.io/mychart",
		},
		{
			name:     "registry already has oci:// prefix",
			registry: "oci://ghcr.io",
			chart:    "mychart",
			version:  "2.0.0",
			want:     "oci://ghcr.io/mychart:2.0.0",
		},
		{
			name:     "registry with trailing slash",
			registry: "ghcr.io/",
			chart:    "mychart",
			version:  "1.0.0",
			want:     "oci://ghcr.io/mychart:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, helm.NormalizeOCIRef(tt.registry, tt.chart, tt.version))
		})
	}
}
