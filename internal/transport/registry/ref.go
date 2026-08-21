package registry

import (
	"fmt"

	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
)

// ParseRef normalizes a "registry/repo[:tag|@digest]" string into a docker
// ImageReference. Bare names get the Docker Hub default and the "latest" tag.
// A reference without a tag or digest is still valid input: the synthetic
// "latest" tag it receives is ignored by tag-listing calls such as
// docker.GetRepositoryTags.
func ParseRef(s string) (types.ImageReference, error) {
	named, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, fmt.Errorf("parse reference %q: %w", s, err)
	}

	ref, err := docker.NewReference(reference.TagNameOnly(named))
	if err != nil {
		return nil, fmt.Errorf("build docker reference for %q: %w", s, err)
	}
	return ref, nil
}
