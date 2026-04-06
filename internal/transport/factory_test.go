package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTransporter struct {
	resourceType domain.ResourceType
}

func (s *stubTransporter) Type() domain.ResourceType { return s.resourceType }
func (s *stubTransporter) Sync(_ context.Context, _ domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
	return nil, nil
}
func (s *stubTransporter) Exists(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
	return false, nil
}
func (s *stubTransporter) ListVersions(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
	return nil, nil
}

func TestNewFactory_RegisteredTypes(t *testing.T) {
	t.Parallel()

	img := &stubTransporter{resourceType: domain.ResourceTypeImage}
	helm := &stubTransporter{resourceType: domain.ResourceTypeHelm}
	git := &stubTransporter{resourceType: domain.ResourceTypeGit}

	f := transport.NewFactory(img, helm, git)
	types := f.RegisteredTypes()

	assert.Len(t, types, 3)
	assert.ElementsMatch(t, []domain.ResourceType{domain.ResourceTypeImage, domain.ResourceTypeHelm, domain.ResourceTypeGit}, types)
}

func TestFactory_ForType_Found(t *testing.T) {
	t.Parallel()

	img := &stubTransporter{resourceType: domain.ResourceTypeImage}
	f := transport.NewFactory(img)

	got, err := f.ForType(domain.ResourceTypeImage)
	require.NoError(t, err)
	assert.Equal(t, domain.ResourceTypeImage, got.Type())
}

func TestFactory_ForType_NotFound(t *testing.T) {
	t.Parallel()

	f := transport.NewFactory()

	_, err := f.ForType(domain.ResourceTypeImage)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnsupportedTransport))
}

func TestFactory_LastTransporterWins(t *testing.T) {
	t.Parallel()

	first := &stubTransporter{resourceType: domain.ResourceTypeImage}
	second := &stubTransporter{resourceType: domain.ResourceTypeImage}

	f := transport.NewFactory(first, second)
	got, err := f.ForType(domain.ResourceTypeImage)
	require.NoError(t, err)

	// The second one should win (last registration)
	assert.Same(t, second, got)
}
