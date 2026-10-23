package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestRegressionConfigRejectsUnknownYAMLFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, yaml string }{
		{"top level", "resoruces: []\n"},
		{"resource", "resources:\n  - type: image\n    source: example.com/probe\n    destination: mirror.example.com/probe\n    tags: [v1]\n    push_mdoe: force\n"},
		{"scanner", "scanners:\n  - name: probe\n    command: echo ok\n    sucess_code: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "probe.config.airgapper.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))
			_, err := config.Load(path)
			assert.Error(t, err, "misspelled settings must not be silently ignored")
		})
	}
}

func TestRegressionValidateScannerReference(t *testing.T) {
	t.Parallel()
	for _, defined := range []bool{false, true} {
		name := "undefined scanner"
		if defined {
			name = "defined scanner"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{Resources: []config.ResourceConfig{{Type: "image", Source: "example.com/probe",
				Destination: "mirror.example.com/probe", Tags: []string{"v1"}, ScannerRef: "probe"}}}
			if defined {
				cfg.Scanners = []config.ScannerConfig{{Name: "probe", Command: "echo ok"}}
			}
			err := config.Validate(cfg)
			if defined {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, domain.ErrInvalidConfig, "scanner references must be validated before any sync starts")
			}
		})
	}
}
