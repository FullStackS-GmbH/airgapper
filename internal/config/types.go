// Package config provides configuration types and loading logic for Universal
// Airgapper. It reads YAML config files, merges multi-file configurations, and
// converts raw config structs into the domain types consumed by the sync engine.
package config

import (
	"strings"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

// Config is the top-level configuration structure. It holds all scanners and
// resources parsed from one or more airgapper YAML files.
type Config struct {
	// Scanners defines external scanner commands that can be referenced by
	// resources via the scanner_ref field.
	Scanners []ScannerConfig `mapstructure:"scanners" yaml:"scanners"`

	// Resources lists all artifacts to synchronize between source and
	// destination registries or repositories.
	Resources []ResourceConfig `mapstructure:"resources" yaml:"resources"`
}

// ScannerConfig defines an external scanner command that can be executed before
// syncing an artifact.
type ScannerConfig struct {
	// Name is the unique identifier for the scanner, referenced by
	// ResourceConfig.ScannerRef.
	Name string `mapstructure:"name" yaml:"name"`

	// Command is the shell command template to execute. It may contain
	// placeholders that the scanner engine substitutes at runtime.
	Command string `mapstructure:"command" yaml:"command"`

	// SuccessCode is the process exit code that indicates a passing scan.
	// Typically 0, but some tools use non-zero codes for "pass with warnings".
	SuccessCode int `mapstructure:"success_code" yaml:"success_code"`

	// Timeout is the maximum number of seconds the scanner is allowed to run.
	// Defaults to 300 if not specified.
	Timeout int `mapstructure:"timeout" yaml:"timeout"`
}

// ResourceConfig is the raw YAML representation of a single resource entry.
// Field usage depends on the Type value; unused fields for a given type are
// silently ignored.
type ResourceConfig struct {
	// Type identifies the artifact kind: "image", "docker", "helm", or "git".
	Type string `mapstructure:"type" yaml:"type"`

	// Source is the full image reference for image/docker resources
	// (e.g. "registry.example.com/repo/image").
	Source string `mapstructure:"source" yaml:"source,omitempty"`

	// Destination is the target image reference for image/docker resources.
	Destination string `mapstructure:"destination" yaml:"destination,omitempty"`

	// Tags lists the image tags to synchronize (image/docker resources only).
	Tags []string `mapstructure:"tags" yaml:"tags,omitempty"`

	// SourceRegistry is the hostname of the source Helm chart registry.
	SourceRegistry string `mapstructure:"source_registry" yaml:"source_registry,omitempty"`

	// SourceChart is the chart name within the source registry (helm resources
	// only).
	SourceChart string `mapstructure:"source_chart" yaml:"source_chart,omitempty"`

	// DestinationRegistry is the hostname of the destination Helm chart
	// registry.
	DestinationRegistry string `mapstructure:"destination_registry" yaml:"destination_registry,omitempty"`

	// DestinationRepo is the repository path within the destination registry.
	// Used by both helm and git resource types. In the YAML file, both types
	// share the "destination_repo" key.
	DestinationRepo string `mapstructure:"destination_repo" yaml:"destination_repo,omitempty"`

	// Versions lists the chart versions to synchronize (helm resources only).
	Versions []string `mapstructure:"versions" yaml:"versions,omitempty"`

	// SourceRepo is the source repository URL for git resources (HTTPS or SSH).
	SourceRepo string `mapstructure:"source_repo" yaml:"source_repo,omitempty"`

	// Refs lists the git refs (branches, tags, SHAs) to synchronize (git
	// resources only).
	Refs []string `mapstructure:"refs" yaml:"refs,omitempty"`

	// PushMode controls how artifacts that already exist at the destination are
	// handled. Valid values are "skip", "force", and "overwrite". Defaults to
	// "skip" when empty.
	PushMode string `mapstructure:"push_mode" yaml:"push_mode,omitempty"`

	// ScannerRef is the optional name of a scanner to run before pushing each
	// version. Must match a ScannerConfig.Name if set.
	ScannerRef string `mapstructure:"scanner_ref" yaml:"scanner_ref,omitempty"`

	// SourceCredentialsRef is the optional name of a credential entry for
	// authenticating against the source.
	SourceCredentialsRef string `mapstructure:"source_credentials_ref" yaml:"source_credentials_ref,omitempty"`

	// TargetCredentialsRef is the optional name of a credential entry for
	// authenticating against the destination.
	TargetCredentialsRef string `mapstructure:"target_credentials_ref" yaml:"target_credentials_ref,omitempty"`

	// Scan is a legacy field from the Python version of the tool. It is
	// accepted during parsing for backward compatibility but otherwise ignored.
	Scan any `mapstructure:"scan" yaml:"scan,omitempty"`
}

// ToResource converts the raw ResourceConfig into a domain.Resource. It maps
// type-specific fields to the unified Resource model and applies defaults.
//
// The "docker" type is treated as an alias for "image". If PushMode is empty
// it defaults to "skip".
func (rc *ResourceConfig) ToResource() domain.Resource {
	r := domain.Resource{
		ScannerRef:           rc.ScannerRef,
		SourceCredentialsRef: rc.SourceCredentialsRef,
		TargetCredentialsRef: rc.TargetCredentialsRef,
	}

	// Resolve push mode, defaulting to skip.
	pm := strings.ToLower(strings.TrimSpace(rc.PushMode))
	if pm == "" {
		r.PushMode = domain.PushModeSkip
	} else {
		r.PushMode = domain.PushMode(pm)
	}

	// Normalize the type string for comparison.
	resType := strings.ToLower(strings.TrimSpace(rc.Type))

	switch resType {
	case "image", "docker":
		r.Type = domain.ResourceTypeImage
		r.Source = parseImageEndpoint(rc.Source)
		r.Destination = parseImageEndpoint(rc.Destination)
		r.Versions = rc.Tags

	case "helm":
		r.Type = domain.ResourceTypeHelm
		r.Source = domain.Endpoint{
			Registry:   rc.SourceRegistry,
			Repository: rc.SourceChart,
		}
		r.Destination = domain.Endpoint{
			Registry:   rc.DestinationRegistry,
			Repository: rc.DestinationRepo,
		}
		r.Versions = rc.Versions

	case "git":
		r.Type = domain.ResourceTypeGit
		r.Source = domain.Endpoint{
			Repository: rc.SourceRepo,
		}
		r.Destination = domain.Endpoint{
			Repository: rc.DestinationRepo,
		}
		r.Versions = rc.Refs
	}

	return r
}

// parseImageEndpoint splits an image reference like
// "registry.example.com/repo/image" into an Endpoint with Registry and
// Repository fields. If there is no slash, the entire string is treated as the
// repository with an empty registry.
func parseImageEndpoint(ref string) domain.Endpoint {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Endpoint{}
	}

	// Split on the first slash. The first segment is the registry if it
	// contains a dot or colon (port), following Docker's convention.
	idx := strings.Index(ref, "/")
	if idx < 0 {
		return domain.Endpoint{Repository: ref}
	}

	firstPart := ref[:idx]
	if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") {
		return domain.Endpoint{
			Registry:   firstPart,
			Repository: ref[idx+1:],
		}
	}

	// No dot or colon in the first segment — treat the whole string as a
	// Docker Hub-style short reference (e.g. "library/ubuntu").
	return domain.Endpoint{Repository: ref}
}
