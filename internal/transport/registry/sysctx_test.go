package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.podman.io/image/v5/types"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
)

func TestSystemContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cred     *domain.Credential
		insecure bool
		certPath string
		want     *types.SystemContext
	}{
		{
			name: "anonymous, secure, no cert path",
			want: &types.SystemContext{},
		},
		{
			name:     "insecure skips TLS verification and ignores cert path",
			insecure: true,
			certPath: "/etc/airgapper/certs.d/internal",
			want: &types.SystemContext{
				DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
				OCIInsecureSkipTLSVerify:    true,
			},
		},
		{
			name:     "cert path sets Docker and OCI cert paths",
			certPath: "/etc/airgapper/certs.d/internal",
			want: &types.SystemContext{
				DockerCertPath: "/etc/airgapper/certs.d/internal",
				OCICertPath:    "/etc/airgapper/certs.d/internal",
			},
		},
		{
			name: "credential sets Docker auth config",
			cred: &domain.Credential{Username: "user", Password: "pass"},
			want: &types.SystemContext{
				DockerAuthConfig: &types.DockerAuthConfig{Username: "user", Password: "pass"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, registry.SystemContext(tt.cred, tt.insecure, tt.certPath))
		})
	}
}
