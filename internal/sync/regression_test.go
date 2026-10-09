package sync_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
)

func TestRegressionPatternExpansionUsesSourceCredentials(t *testing.T) {
	t.Parallel()
	for _, resourceType := range []domain.ResourceType{domain.ResourceTypeImage, domain.ResourceTypeHelm, domain.ResourceTypeGit} {
		for _, explicit := range []bool{false, true} {
			name := fmt.Sprintf("%s/host lookup", resourceType)
			if explicit {
				name = fmt.Sprintf("%s/explicit reference", resourceType)
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				source := domain.Endpoint{Registry: "private.example.com", Repository: "probe"}
				if resourceType == domain.ResourceTypeGit {
					source = domain.Endpoint{Repository: "https://private.example.com/probe.git"}
				}
				credentialName := source.Registry
				if resourceType == domain.ResourceTypeGit {
					credentialName = source.Repository
				}
				ref := ""
				if explicit {
					credentialName, ref = "source", "source"
				}
				credential := domain.Credential{Name: credentialName, Type: domain.CredentialType(resourceType), Username: "probe", Password: "dummy"}
				transporter := &mockTransporter{
					typeFn: func() domain.ResourceType { return resourceType },
					listVersionsFn: func(_ context.Context, _ domain.Endpoint, got *domain.Credential) ([]string, error) {
						assert.Equal(t, &credential, got, "pattern listing must use the same source credentials as literal sync")
						if got == nil {
							return nil, domain.ErrAuthFailed
						}
						return []string{"v1"}, nil
					},
					syncFn: func(_ context.Context, resource domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
						return &domain.SyncResult{Resource: resource, Synced: []domain.VersionResult{{Version: "v1", Status: domain.SyncStatusSynced}}}, nil
					},
				}
				engine := sync.NewEngine([]domain.Transporter{transporter}, nil, discardLogger())
				results, err := engine.Run(context.Background(), []domain.Resource{{Type: resourceType, Source: source,
					Versions: []string{"v.*"}, SourceCredentialsRef: ref}}, domain.SyncOptions{Credentials: credentials.NewFileStore([]domain.Credential{credential})})
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.Empty(t, results[0].Failed)
				assert.Len(t, results[0].Synced, 1)
			})
		}
	}
}
