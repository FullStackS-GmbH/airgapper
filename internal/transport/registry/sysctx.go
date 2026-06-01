// Package registry provides shared helpers for the containers/image
// (go.podman.io/image/v5) library used by the image and helm transporters.
package registry

import (
	"go.podman.io/image/v5/types"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// SystemContext builds a *types.SystemContext suitable for docker:// transport
// operations. A nil credential yields anonymous access. When insecure is true,
// TLS verification is skipped and plain HTTP is allowed (mirrors
// crane.WithInsecure semantics).
func SystemContext(cred *domain.Credential, insecure bool) *types.SystemContext {
	sys := &types.SystemContext{}
	if insecure {
		sys.DockerInsecureSkipTLSVerify = types.OptionalBoolTrue
		sys.OCIInsecureSkipTLSVerify = true
	}
	if cred != nil {
		sys.DockerAuthConfig = &types.DockerAuthConfig{
			Username: cred.Username,
			Password: cred.Password,
		}
	}
	return sys
}
