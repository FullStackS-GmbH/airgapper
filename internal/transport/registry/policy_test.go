package registry

import (
	"context"
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
