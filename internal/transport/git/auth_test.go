package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestCredToTransportAuth_NilCredentialIsAnonymous(t *testing.T) {
	t.Parallel()

	auth, err := credToTransportAuth(nil)
	require.NoError(t, err)
	assert.Nil(t, auth, "a nil credential must yield anonymous access, not empty basic auth")
}

func TestCredToTransportAuth_BasicAuth(t *testing.T) {
	t.Parallel()

	auth, err := credToTransportAuth(&domain.Credential{
		Name:     "github",
		Type:     domain.CredentialTypeGit,
		Username: "octocat",
		Password: "ghp_token",
	})
	require.NoError(t, err)

	basic, ok := auth.(*http.BasicAuth)
	require.True(t, ok, "expected HTTPS basic auth, got %T", auth)
	assert.Equal(t, "octocat", basic.Username)
	assert.Equal(t, "ghp_token", basic.Password)
}

func TestCredToTransportAuth_AzureSentinelReadsEnvVar(t *testing.T) {
	// The Azure Repos workaround treats Password as the *name* of an
	// environment variable holding the real token, not the token itself.
	t.Setenv("AZURE_REPOS_TOKEN", "the-real-token")

	auth, err := credToTransportAuth(&domain.Credential{
		Type:     domain.CredentialTypeGit,
		Username: azureReposAuthSentinel,
		Password: "AZURE_REPOS_TOKEN",
	})
	require.NoError(t, err)

	basic, ok := auth.(*http.BasicAuth)
	require.True(t, ok, "expected HTTPS basic auth, got %T", auth)
	assert.Equal(t, azureReposAuthSentinel, basic.Username)
	assert.Equal(t, "the-real-token", basic.Password)
	assert.NotEqual(t, "AZURE_REPOS_TOKEN", basic.Password,
		"the env var name must never be sent as the password")
}

func TestCredToTransportAuth_AzureSentinelWithUnsetEnvVar(t *testing.T) {
	// An unset variable yields an empty password rather than leaking the name.
	// Authentication then fails at the remote, which is the correct outcome.
	auth, err := credToTransportAuth(&domain.Credential{
		Type:     domain.CredentialTypeGit,
		Username: azureReposAuthSentinel,
		Password: "DEFINITELY_NOT_SET_AIRGAPPER_TEST",
	})
	require.NoError(t, err)

	basic, ok := auth.(*http.BasicAuth)
	require.True(t, ok)
	assert.Empty(t, basic.Password)
}

func TestCredToTransportAuth_SSHKeyTakesPrecedence(t *testing.T) {
	t.Parallel()

	// A credential carrying an SSH key must take the SSH branch even when a
	// username and password are also present. A missing key file is an error,
	// not a silent fallback to basic auth with the wrong secret.
	missing := filepath.Join(t.TempDir(), "id_ed25519")

	auth, err := credToTransportAuth(&domain.Credential{
		Type:       domain.CredentialTypeGit,
		Username:   "octocat",
		Password:   "ghp_token",
		SSHKeyPath: missing,
	})

	require.Error(t, err)
	assert.Nil(t, auth)
	assert.Contains(t, err.Error(), missing, "the error must name the key path it could not load")
}

func TestCredToTransportAuth_SSHKeyLoads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "id_ed25519")
	writeTestSSHKey(t, path)

	auth, err := credToTransportAuth(&domain.Credential{
		Type:       domain.CredentialTypeGit,
		SSHKeyPath: path,
	})
	require.NoError(t, err)
	require.NotNil(t, auth)
	assert.Equal(t, "ssh-public-keys", auth.Name())
}

// writeTestSSHKey generates a throwaway unencrypted ed25519 key in OpenSSH
// format at path. Generated rather than checked in so no private key material,
// however inert, lives in the repository.
func writeTestSSHKey(t *testing.T, path string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := ssh.MarshalPrivateKey(priv, "airgapper-test-key")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))
}

func TestGitHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint domain.Endpoint
		want     string
	}{
		{
			name:     "registry wins when set",
			endpoint: domain.Endpoint{Registry: "github.com", Repository: "https://github.com/org/repo.git"},
			want:     "github.com",
		},
		{
			name:     "falls back to the repository URL",
			endpoint: domain.Endpoint{Repository: "https://github.com/org/repo.git"},
			want:     "https://github.com/org/repo.git",
		},
		{
			name:     "empty endpoint",
			endpoint: domain.Endpoint{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, gitHost(tt.endpoint))
		})
	}
}
