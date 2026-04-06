package credentials_test

import (
	"errors"
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCredentials() []domain.Credential {
	return []domain.Credential{
		{Name: "docker-hub", Type: domain.CredentialTypeImage, Username: "user1", Password: "pass1"},
		{Name: "helm-reg", Type: domain.CredentialTypeHelm, Username: "helmuser", Password: "helmpass"},
		{Name: "github", Type: domain.CredentialTypeGit, Username: "gituser", Password: "gittoken"},
	}
}

func TestNewFileStore(t *testing.T) {
	t.Parallel()

	store := credentials.NewFileStore(testCredentials())
	require.NotNil(t, store)
}

func TestNewFileStore_NilCredentials(t *testing.T) {
	t.Parallel()

	store := credentials.NewFileStore(nil)
	require.NotNil(t, store)

	// Should return nil for any lookup
	cred, err := store.Resolve("docker-hub", domain.CredentialTypeImage)
	assert.NoError(t, err)
	assert.Nil(t, cred)
}

func TestResolve_ByHostAndType(t *testing.T) {
	t.Parallel()

	store := credentials.NewFileStore(testCredentials())

	tests := []struct {
		name     string
		host     string
		credType domain.CredentialType
		wantName string
		wantNil  bool
	}{
		{
			name:     "image credential found",
			host:     "docker-hub",
			credType: domain.CredentialTypeImage,
			wantName: "docker-hub",
		},
		{
			name:     "helm credential found",
			host:     "helm-reg",
			credType: domain.CredentialTypeHelm,
			wantName: "helm-reg",
		},
		{
			name:     "git credential found",
			host:     "github",
			credType: domain.CredentialTypeGit,
			wantName: "github",
		},
		{
			name:     "wrong type returns nil",
			host:     "docker-hub",
			credType: domain.CredentialTypeGit,
			wantNil:  true,
		},
		{
			name:     "unknown host returns nil",
			host:     "unknown",
			credType: domain.CredentialTypeImage,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cred, err := store.Resolve(tt.host, tt.credType)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, cred)
			} else {
				require.NotNil(t, cred)
				assert.Equal(t, tt.wantName, cred.Name)
			}
		})
	}
}

func TestResolveByRef_Found(t *testing.T) {
	t.Parallel()

	store := credentials.NewFileStore(testCredentials())

	cred, err := store.ResolveByRef("docker-hub")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "docker-hub", cred.Name)
	assert.Equal(t, "user1", cred.Username)
}

func TestResolveByRef_NotFound(t *testing.T) {
	t.Parallel()

	store := credentials.NewFileStore(testCredentials())

	_, err := store.ResolveByRef("nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrCredentialNotFound))
}
