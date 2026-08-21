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
// crane.WithInsecure semantics). When certPath is non-empty, it names a
// directory containing a "ca.crt" file (and optionally cert.pem/key.pem)
// trusted for this endpoint in addition to the system pool, following
// Docker's host-cert-directory convention; it is ignored when insecure is
// true, since skipping verification makes it moot.
func SystemContext(cred *domain.Credential, insecure bool, certPath string) *types.SystemContext {
	sys := &types.SystemContext{}
	switch {
	case insecure:
		sys.DockerInsecureSkipTLSVerify = types.OptionalBoolTrue
		sys.OCIInsecureSkipTLSVerify = true
	case certPath != "":
		sys.DockerCertPath = certPath
		sys.OCICertPath = certPath
	}
	if cred != nil {
		sys.DockerAuthConfig = &types.DockerAuthConfig{
			Username: cred.Username,
			Password: cred.Password,
		}
	}
	return sys
}
