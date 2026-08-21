package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
)

func TestPolicyContext_EmptyPathIsPermissive(t *testing.T) {
	t.Parallel()

	got, err := registry.PolicyContext("")
	require.NoError(t, err)

	want, err := registry.PermissivePolicyContext()
	require.NoError(t, err)
	assert.Same(t, want, got)
}

func TestPolicyContext_LoadsPolicyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"default":[{"type":"reject"}]}`), 0o600))

	got, err := registry.PolicyContext(path)
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestPolicyContext_MissingFileErrors(t *testing.T) {
	t.Parallel()

	_, err := registry.PolicyContext("/nonexistent/policy.json")
	assert.Error(t, err)
}

func TestPolicyContext_InvalidPolicyErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o600))

	_, err := registry.PolicyContext(path)
	assert.Error(t, err)
}
