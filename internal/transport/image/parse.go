// Package image implements the container image transport layer using the
// go-containerregistry library for registry-to-registry image copies.
package image

import (
	"github.com/google/go-containerregistry/pkg/name"
)

// ParseImageRef normalizes an image reference string into a full
// registry/repository form. Docker Hub short names are expanded to their
// canonical form:
//
//   - "ubuntu"                          → "registry-1.docker.io", "library/ubuntu"
//   - "myrepo/image"                    → "registry-1.docker.io", "myrepo/image"
//   - "registry.example.com/repo/image" → "registry.example.com", "repo/image"
//
// It uses name.ParseReference from go-containerregistry which handles Docker
// Hub defaulting automatically.
func ParseImageRef(ref string) (registry string, repository string) {
	// name.ParseReference handles Docker Hub defaults, adding
	// "registry-1.docker.io" and "library/" prefix as needed.
	parsed, err := name.ParseReference(ref)
	if err != nil {
		// Fallback: return the ref as-is in the repository field.
		return "", ref
	}

	registry = parsed.Context().RegistryStr()
	repository = parsed.Context().RepositoryStr()
	return registry, repository
}
