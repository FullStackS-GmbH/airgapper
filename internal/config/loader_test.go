package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_SingleFile(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load("testdata/single.config.airgapper.yaml")
	require.NoError(t, err)

	assert.Len(t, cfg.Scanners, 1)
	assert.Equal(t, "trivy", cfg.Scanners[0].Name)
	assert.Equal(t, 120, cfg.Scanners[0].Timeout)

	assert.Len(t, cfg.Resources, 1)
	assert.Equal(t, "image", cfg.Resources[0].Type)
	assert.Equal(t, "registry.example.com/repo/image", cfg.Resources[0].Source)
	assert.Equal(t, []string{"v1.0.0", "v2.0.0"}, cfg.Resources[0].Tags)
}

func TestLoad_Directory(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load("testdata")
	require.NoError(t, err)

	// Should merge single.airgapper.yaml and extra.airgapper.yaml
	assert.Len(t, cfg.Scanners, 1)
	assert.Len(t, cfg.Resources, 2)
}

func TestLoad_MissingPath(t *testing.T) {
	t.Parallel()

	_, err := config.Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
}

func TestLoad_DirectoryNoConfigFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := config.Load(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no airgapper config files found")
}

func TestLoad_TempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte(`resources:
  - type: git
    source_repo: "https://github.com/org/repo.git"
    destination_repo: "https://internal.git/org/repo.git"
    refs:
      - "main"
`)
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Len(t, cfg.Resources, 1)
	assert.Equal(t, "git", cfg.Resources[0].Type)
}

func TestLoadCredentials_Valid(t *testing.T) {
	t.Parallel()

	creds, err := config.LoadCredentials("testdata/credentials.yaml")
	require.NoError(t, err)

	assert.Len(t, creds, 3)

	// Check image credential
	assert.Equal(t, "docker-hub", creds[0].Name)
	assert.Equal(t, "user1", creds[0].Username)
	assert.Equal(t, "pass1", creds[0].Password)

	// Check git credential has ssh_key_path
	var gitCred *struct{ Name, SSHKeyPath string }
	for _, c := range creds {
		if c.Name == "github" {
			gitCred = &struct{ Name, SSHKeyPath string }{c.Name, c.SSHKeyPath}
			break
		}
	}
	require.NotNil(t, gitCred)
	assert.Equal(t, "/path/to/key", gitCred.SSHKeyPath)
}

func TestLoadCredentials_Empty(t *testing.T) {
	t.Parallel()

	creds, err := config.LoadCredentials("testdata/empty_credentials.yaml")
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestLoadCredentials_Directory(t *testing.T) {
	t.Parallel()

	creds, err := config.LoadCredentials("testdata/creds_dir")
	require.NoError(t, err)

	// Should merge image.creds.airgapper.yaml + git.creds.airgapper.yaml
	assert.Len(t, creds, 2)

	names := make(map[string]bool)
	for _, c := range creds {
		names[c.Name] = true
	}
	assert.True(t, names["docker-hub"])
	assert.True(t, names["github"])
}

func TestLoadCredentials_DirectoryNoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := config.LoadCredentials(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credential files")
}

func TestLoadCredentials_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := config.LoadCredentials("/nonexistent/credentials.yaml")
	require.Error(t, err)
}
