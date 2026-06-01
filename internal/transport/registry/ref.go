package registry

import (
	"fmt"

	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
)

// ParseDockerRef normalizes a registry/repo[:tag|@digest] string and returns
// both the docker ImageReference (for transport operations) and the underlying
// Named (for inspecting registry/repository parts). Bare names get the Docker
// Hub default and the "latest" tag, matching go-containerregistry's
// name.ParseReference behavior.
func ParseDockerRef(s string) (types.ImageReference, reference.Named, error) {
	named, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, nil, fmt.Errorf("parse reference %q: %w", s, err)
	}
	named = reference.TagNameOnly(named)
	ref, err := docker.NewReference(named)
	if err != nil {
		return nil, nil, fmt.Errorf("build docker reference for %q: %w", s, err)
	}
	return ref, named, nil
}

// ParseRepoRef parses a "registry/repo" string without a tag/digest, returning
// a Named usable with docker.GetRepositoryTags.
func ParseRepoRef(s string) (types.ImageReference, reference.Named, error) {
	named, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, nil, fmt.Errorf("parse repo %q: %w", s, err)
	}
	// docker.NewReference requires a tag or digest; attach a synthetic tag so
	// we can build a reference, then strip semantics by passing it to
	// GetRepositoryTags which ignores the tag.
	tagged := reference.TagNameOnly(named)
	ref, err := docker.NewReference(tagged)
	if err != nil {
		return nil, nil, fmt.Errorf("build docker reference for %q: %w", s, err)
	}
	return ref, named, nil
}
