package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestValidate_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Scanners: []config.ScannerConfig{
			{Name: "trivy", Command: "trivy image {source}", SuccessCode: 0},
		},
		Resources: []config.ResourceConfig{
			{
				Type:        "image",
				Source:      "registry.example.com/repo/image",
				Destination: "internal.registry.io/repo/image",
				Tags:        []string{"v1.0.0"},
			},
			{
				Type:                "helm",
				SourceRegistry:      "charts.example.com",
				SourceChart:         "mychart",
				DestinationRegistry: "internal.charts.io",
				DestinationRepo:     "mychart",
				Versions:            []string{"1.0.0"},
			},
			{
				Type:            "git",
				SourceRepo:      "https://github.com/org/repo.git",
				DestinationRepo: "https://internal.git/org/repo.git",
				Refs:            []string{"main"},
			},
		},
	}

	err := config.Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_NilConfig(t *testing.T) {
	t.Parallel()

	err := config.Validate(nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
}

func TestValidate_ImageMissingSource(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:        "image",
				Destination: "internal.registry.io/repo/image",
				Tags:        []string{"v1.0.0"},
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "source is required")
}

func TestValidate_ImageMissingTags(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:        "image",
				Source:      "registry.example.com/repo/image",
				Destination: "internal.registry.io/repo/image",
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "at least one tag is required")
}

func TestValidate_HelmMissingFields(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:            "helm",
				SourceChart:     "mychart",
				DestinationRepo: "mychart",
				Versions:        []string{"1.0.0"},
				// Missing source_registry and destination_registry
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
}

func TestValidate_HelmMissingVersions(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:                "helm",
				SourceRegistry:      "charts.example.com",
				SourceChart:         "mychart",
				DestinationRegistry: "internal.charts.io",
				DestinationRepo:     "mychart",
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "at least one version is required")
}

func TestValidate_HelmDestinationChartRejectsRepositoryPath(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Resources: []config.ResourceConfig{{
		Type:                "helm",
		SourceRegistry:      "registry.suse.com",
		SourceChart:         "private-registry/1.2/private-registry-helm",
		DestinationRegistry: "registry.internal",
		DestinationRepo:     "platform-charts",
		DestinationChart:    "nested/suse-private-registry",
		Versions:            []string{"1.2.1"},
	}}}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "destination_chart must be a chart name")
}

func TestValidate_GitMissingFields(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type: "git",
				Refs: []string{"main"},
				// Missing source_repo and destination_repo
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
}

func TestValidate_GitMissingRefs(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:            "git",
				SourceRepo:      "https://github.com/org/repo.git",
				DestinationRepo: "https://internal.git/org/repo.git",
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "at least one ref is required")
}

func TestValidate_MissingType(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Source:      "registry.example.com/repo/image",
				Destination: "internal.registry.io/repo/image",
				Tags:        []string{"v1.0.0"},
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "type is required")
}

func TestValidate_UnknownType(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type: "ftp",
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "unknown resource type")
}

func TestValidate_InvalidPushMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:        "image",
				Source:      "registry.example.com/repo/image",
				Destination: "internal.registry.io/repo/image",
				Tags:        []string{"v1.0.0"},
				PushMode:    "invalid",
			},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "invalid push_mode")
}

func TestValidate_DuplicateScannerNames(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Scanners: []config.ScannerConfig{
			{Name: "trivy", Command: "trivy image {source}"},
			{Name: "trivy", Command: "trivy image --severity HIGH {source}"},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "duplicate name")
}

func TestValidate_EmptyScannerName(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Scanners: []config.ScannerConfig{
			{Name: "", Command: "trivy image {source}"},
		},
	}

	err := config.Validate(cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidConfig))
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidate_DockerTypeAlias(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Resources: []config.ResourceConfig{
			{
				Type:        "docker",
				Source:      "registry.example.com/repo/image",
				Destination: "internal.registry.io/repo/image",
				Tags:        []string{"v1.0.0"},
			},
		},
	}

	err := config.Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_UnknownScannerRef(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Scanners: []config.ScannerConfig{{Name: "trivy", Command: "trivy"}},
		Resources: []config.ResourceConfig{{
			Type: "image", Source: "a.io/x", Destination: "b.io/x", Tags: []string{"v1"}, ScannerRef: "trivvy",
		}},
	}

	err := config.Validate(cfg)

	require.ErrorIs(t, err, domain.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "trivvy")

	cfg.Resources[0].ScannerRef = "trivy"
	require.NoError(t, config.Validate(cfg))
}

func TestValidate_OptionsThatDoNotApplyToTheResourceType(t *testing.T) {
	t.Parallel()

	image := config.ResourceConfig{Type: "image", Source: "a.io/x", Destination: "b.io/x", Tags: []string{"v1"}}
	helm := config.ResourceConfig{Type: "helm", SourceRegistry: "a.io", SourceChart: "c", DestinationRegistry: "b.io", DestinationRepo: "r", Versions: []string{"1"}}
	git := config.ResourceConfig{Type: "git", SourceRepo: "https://a/x.git", DestinationRepo: "https://b/x.git", Refs: []string{"main"}}

	for _, tc := range []struct {
		name   string
		mutate func() config.ResourceConfig
		want   string
	}{
		{"policy_path on helm", func() config.ResourceConfig { r := helm; r.PolicyPath = "p.json"; return r }, "policy_path"},
		{"policy_path on git", func() config.ResourceConfig { r := git; r.PolicyPath = "p.json"; return r }, "policy_path"},
		{"destination_chart on image", func() config.ResourceConfig { r := image; r.DestinationChart = "c"; return r }, "destination_chart"},
		{"source_insecure on git", func() config.ResourceConfig { r := git; r.SourceInsecure = true; return r }, "insecure"},
		{"destination_ca_cert on git", func() config.ResourceConfig { r := git; r.DestinationCACert = "/ca"; return r }, "ca_cert"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := config.Validate(&config.Config{Resources: []config.ResourceConfig{tc.mutate()}})
			require.ErrorIs(t, err, domain.ErrInvalidConfig)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("applicable options stay valid", func(t *testing.T) {
		t.Parallel()
		img := image
		img.PolicyPath, img.SourceInsecure = "p.json", true
		h := helm
		h.DestinationChart, h.DestinationInsecure = "c", true
		require.NoError(t, config.Validate(&config.Config{Resources: []config.ResourceConfig{img, h, git}}))
	})
}

func TestValidate_ReportsEveryProblem(t *testing.T) {
	t.Parallel()

	err := config.Validate(&config.Config{Resources: []config.ResourceConfig{
		{Type: "image", Source: "a.io/x", Destination: "b.io/x"},
		{Type: "bogus"},
		{Type: "git", SourceRepo: "https://a/x.git"},
	}})

	require.ErrorIs(t, err, domain.ErrInvalidConfig)
	for _, want := range []string{"resource[0]", "resource[1]", "resource[2]"} {
		assert.Contains(t, err.Error(), want)
	}
}
