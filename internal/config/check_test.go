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

func imageResource(mutate func(*config.ResourceConfig)) *config.Config {
	rc := config.ResourceConfig{Type: "image", Source: "a.io/x", Destination: "b.io/x", Tags: []string{"v1"}}
	mutate(&rc)
	return &config.Config{Resources: []config.ResourceConfig{rc}}
}

func TestCheck_CredentialReferences(t *testing.T) {
	t.Parallel()

	creds := []domain.Credential{{Name: "reg", Type: domain.CredentialTypeImage}}

	t.Run("known reference", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.TargetCredentialsRef = "reg" })
		require.NoError(t, config.Check(cfg, creds, true))
	})

	t.Run("unknown reference", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.TargetCredentialsRef = "nope" })
		err := config.Check(cfg, creds, true)
		require.ErrorIs(t, err, domain.ErrInvalidConfig)
		assert.Contains(t, err.Error(), "target_credentials_ref")
		assert.Contains(t, err.Error(), "nope")
	})

	t.Run("reference of another credential type", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) {
			rc.Type = "git"
			rc.SourceRepo = "x"
			rc.DestinationRepo = "y"
			rc.SourceCredentialsRef = "reg"
		})
		require.Error(t, config.Check(cfg, creds, true), "an image credential must not satisfy a git resource")
	})

	t.Run("reference without a credentials file", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.SourceCredentialsRef = "reg" })
		err := config.Check(cfg, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--credentials")
	})
}

func TestCheck_Files(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(policy, []byte("{}"), 0o600))
	caDir := filepath.Join(dir, "ca")
	require.NoError(t, os.Mkdir(caDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(caDir, "ca.crt"), []byte("x"), 0o600))

	t.Run("existing files", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.PolicyPath = policy; rc.SourceCACert = caDir })
		require.NoError(t, config.Check(cfg, nil, false))
	})

	t.Run("missing policy file", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.PolicyPath = filepath.Join(dir, "gone.json") })
		err := config.Check(cfg, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policy_path")
	})

	t.Run("CA directory without ca.crt", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) { rc.DestinationCACert = dir })
		err := config.Check(cfg, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "destination_ca_cert")
	})

	t.Run("ssh key of a referenced credential", func(t *testing.T) {
		t.Parallel()
		creds := []domain.Credential{
			{Name: "used", Type: domain.CredentialTypeGit, SSHKeyPath: filepath.Join(dir, "missing-key")},
			{Name: "unused", Type: domain.CredentialTypeGit, SSHKeyPath: filepath.Join(dir, "also-missing")},
		}
		cfg := imageResource(func(rc *config.ResourceConfig) {
			rc.Type, rc.SourceRepo, rc.DestinationRepo, rc.SourceCredentialsRef = "git", "x", "y", "used"
		})
		err := config.Check(cfg, creds, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ssh key")
		assert.NotContains(t, err.Error(), "also-missing", "an unreferenced credential must not fail the check")
	})

	t.Run("reports every problem", func(t *testing.T) {
		t.Parallel()
		cfg := imageResource(func(rc *config.ResourceConfig) {
			rc.PolicyPath = filepath.Join(dir, "gone.json")
			rc.SourceCACert = dir
			rc.TargetCredentialsRef = "nope"
		})
		err := config.Check(cfg, nil, true)
		require.Error(t, err)
		for _, want := range []string{"policy_path", "source_ca_cert", "target_credentials_ref"} {
			assert.Contains(t, err.Error(), want)
		}
	})
}
