// Package image implements the container image transport layer using the
// containers/image (go.podman.io/image/v5) library for registry-to-registry
// image copies.
package image

import (
	"go.podman.io/image/v5/docker/reference"
)

// ParseImageRef normalizes an image reference string into a full
// registry/repository form. Docker Hub short names are expanded to their
// canonical form:
//
//   - "ubuntu"                          → "docker.io", "library/ubuntu"
//   - "myrepo/image"                    → "docker.io", "myrepo/image"
//   - "registry.example.com/repo/image" → "registry.example.com", "repo/image"
//
// It uses reference.ParseNormalizedNamed which handles Docker Hub defaulting
// automatically (note: the canonical domain is "docker.io", not
// "registry-1.docker.io").
func ParseImageRef(ref string) (registry string, repository string) {
	parsed, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		// Fallback: return the ref as-is in the repository field.
		return "", ref
	}

	return reference.Domain(parsed), reference.Path(parsed)
}
