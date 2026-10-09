package registry

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
	"golang.org/x/sync/errgroup"
)

// policyTestImage pauses evaluation after the context enters its InUse state.
type policyTestImage struct {
	types.UnparsedImage
	ref     types.ImageReference
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (i *policyTestImage) Reference() types.ImageReference {
	if i.entered != nil {
		i.once.Do(func() { close(i.entered) })
		<-i.release
	}
	return i.ref
}

func TestPermissivePolicyContextsEvaluateIndependently(t *testing.T) {
	t.Parallel()
	first, err := PermissivePolicyContext()
	require.NoError(t, err)
	second, err := PermissivePolicyContext()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, first.Destroy())
		if first != second {
			require.NoError(t, second.Destroy())
		}
	}()
	assert.NotSame(t, first, second)
	ref, err := ParseRef("docker.io/library/alpine:3.21")
	require.NoError(t, err)
	image := &policyTestImage{ref: ref, entered: make(chan struct{}), release: make(chan struct{})}
	var group errgroup.Group
	group.Go(func() error {
		allowed, err := first.IsRunningImageAllowed(context.Background(), image)
		assert.True(t, allowed)
		return err
	})
	<-image.entered
	allowed, evalErr := second.IsRunningImageAllowed(context.Background(), &policyTestImage{ref: ref})
	close(image.release)
	assert.NoError(t, evalErr)
	assert.True(t, allowed)
	require.NoError(t, group.Wait())
}

func TestPolicyContext_EmptyPathIsPermissive(t *testing.T) {
	t.Parallel()

	got, err := PolicyContext("")
	require.NoError(t, err)

	want, err := PermissivePolicyContext()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, got.Destroy())
		require.NoError(t, want.Destroy())
	}()
	assert.NotSame(t, want, got)
	ref, err := ParseRef("docker.io/library/alpine:3.21")
	require.NoError(t, err)
	allowed, err := got.IsRunningImageAllowed(context.Background(), &policyTestImage{ref: ref})
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestPolicyContext_LoadsPolicyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"default":[{"type":"reject"}]}`), 0o600))

	got, err := PolicyContext(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	defer func() { require.NoError(t, got.Destroy()) }()
	ref, err := ParseRef("docker.io/library/alpine:3.21")
	require.NoError(t, err)
	allowed, err := got.IsRunningImageAllowed(context.Background(), &policyTestImage{ref: ref})
	assert.Error(t, err)
	assert.False(t, allowed)
}

func TestPolicyContext_MissingFileErrors(t *testing.T) {
	t.Parallel()

	_, err := PolicyContext("/nonexistent/policy.json")
	assert.Error(t, err)
}

func TestPolicyContext_InvalidPolicyErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o600))

	_, err := PolicyContext(path)
	assert.Error(t, err)
}
