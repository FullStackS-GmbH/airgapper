// Package config provides configuration types and loading logic for Universal
// Airgapper. It reads YAML config files, merges multi-file configurations, and
// converts raw config structs into the domain types consumed by the sync engine.
package config

import (
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// Config is the top-level configuration structure. It holds all scanners and
// resources parsed from one or more airgapper YAML files.
type Config struct {
	// Scanners defines external scanner commands that can be referenced by
	// resources via the scanner_ref field.
	Scanners []ScannerConfig `yaml:"scanners"`

	// Resources lists all artifacts to synchronize between source and
	// destination registries or repositories.
	Resources []ResourceConfig `yaml:"resources"`
}

// ScannerConfig defines an external scanner command that can be executed before
// syncing an artifact.
type ScannerConfig struct {
	// Name is the unique identifier for the scanner, referenced by
	// ResourceConfig.ScannerRef.
	Name string `yaml:"name"`

	// Command is the shell command template to execute. It may contain
	// placeholders that the scanner engine substitutes at runtime.
	Command string `yaml:"command"`

	// SuccessCode is the process exit code that indicates a passing scan.
	// Typically 0, but some tools use non-zero codes for "pass with warnings".
	SuccessCode int `yaml:"success_code"`

	// Timeout is the maximum number of seconds the scanner is allowed to run.
	// Defaults to 300 if not specified.
	Timeout int `yaml:"timeout"`
}

// ResourceConfig is the raw YAML representation of a single resource entry.
// Field usage depends on the Type value; unused fields for a given type are
// silently ignored.
type ResourceConfig struct {
	// Type identifies the artifact kind: "image", "docker", "helm", or "git".
	Type string `yaml:"type"`

	// Source is the full image reference for image/docker resources
	// (e.g. "registry.example.com/repo/image").
	Source string `yaml:"source,omitempty"`

	// Destination is the target image reference for image/docker resources.
	Destination string `yaml:"destination,omitempty"`

	// Tags lists the image tags to synchronize (image/docker resources only).
	Tags []string `yaml:"tags,omitempty"`

	// SourceRegistry is the hostname of the source Helm chart registry.
	SourceRegistry string `yaml:"source_registry,omitempty"`

	// SourceChart is the chart name within the source registry (helm resources
	// only).
	SourceChart string `yaml:"source_chart,omitempty"`

	// DestinationRegistry is the hostname of the destination Helm chart
	// registry.
	DestinationRegistry string `yaml:"destination_registry,omitempty"`

	// DestinationRepo is the repository path within the destination registry.
	// Used by both helm and git resource types. In the YAML file, both types
	// share the "destination_repo" key.
	DestinationRepo string `yaml:"destination_repo,omitempty"`

	// DestinationChart optionally overrides the chart name at the destination.
	// For Helm resources, the chart archive metadata is updated to match.
	DestinationChart string `yaml:"destination_chart,omitempty"`

	// Versions lists the chart versions to synchronize (helm resources only).
	Versions []string `yaml:"versions,omitempty"`

	// SourceRepo is the source repository URL for git resources (HTTPS or SSH).
	SourceRepo string `yaml:"source_repo,omitempty"`

	// Refs lists the git refs (branches, tags, SHAs) to synchronize (git
	// resources only).
	Refs []string `yaml:"refs,omitempty"`

	// PushMode controls how artifacts that already exist at the destination are
	// handled. Valid values are "skip", "force", and "overwrite". Defaults to
	// "skip" when empty.
	PushMode string `yaml:"push_mode,omitempty"`

	// ScannerRef is the optional name of a scanner to run before pushing each
	// version. Must match a ScannerConfig.Name if set.
	ScannerRef string `yaml:"scanner_ref,omitempty"`

	// PolicyPath is the optional path to a containers/image signature
	// verification policy (policy.json) to check the source image against
	// before pushing. Image resources only. Empty means accept any image
	// unverified (the pre-existing default).
	PolicyPath string `yaml:"policy_path,omitempty"`

	// SourceCredentialsRef is the optional name of a credential entry for
	// authenticating against the source.
	SourceCredentialsRef string `yaml:"source_credentials_ref,omitempty"`

	// TargetCredentialsRef is the optional name of a credential entry for
	// authenticating against the destination.
	TargetCredentialsRef string `yaml:"target_credentials_ref,omitempty"`

	// SourceInsecure skips TLS certificate verification against the source
	// endpoint (and, for Helm, forces plain HTTP). Off by default. Prefer
	// SourceCACert when possible.
	SourceInsecure bool `yaml:"source_insecure,omitempty"`

	// DestinationInsecure skips TLS certificate verification against the
	// destination endpoint (and, for Helm, forces plain HTTP). Off by
	// default. Prefer DestinationCACert when possible.
	DestinationInsecure bool `yaml:"destination_insecure,omitempty"`

	// SourceCACert is a directory containing a "ca.crt" file trusted for the
	// source endpoint, in addition to the system pool (Docker cert-path
	// convention). Lets a private CA be trusted without disabling
	// verification.
	SourceCACert string `yaml:"source_ca_cert,omitempty"`

	// DestinationCACert is a directory containing a "ca.crt" file trusted
	// for the destination endpoint, in addition to the system pool.
	DestinationCACert string `yaml:"destination_ca_cert,omitempty"`
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

	// Resolve push mode. Unknown values are rejected by config.Validate; fall
	// back to the safe default here so a caller that skipped validation cannot
	// end up overwriting the destination.
	pm, ok := domain.ParsePushMode(rc.PushMode)
	if !ok {
		pm = domain.PushModeSkip
	}
	r.PushMode = pm

	// Normalize the type string for comparison.
	resType := strings.ToLower(strings.TrimSpace(rc.Type))

	switch resType {
	case "image", "docker":
		r.Type = domain.ResourceTypeImage
		r.Source = parseImageEndpoint(rc.Source)
		r.Destination = parseImageEndpoint(rc.Destination)
		r.Versions = rc.Tags
		r.PolicyPath = strings.TrimSpace(rc.PolicyPath)
		rc.applyTLSOptions(&r.Source, &r.Destination)

	case "helm":
		r.Type = domain.ResourceTypeHelm
		r.DestinationChart = strings.Trim(strings.TrimSpace(rc.DestinationChart), "/")
		r.Source = domain.Endpoint{
			Registry:   strings.TrimRight(strings.TrimSpace(rc.SourceRegistry), "/"),
			Repository: strings.Trim(strings.TrimSpace(rc.SourceChart), "/"),
		}
		r.Destination = domain.Endpoint{
			Registry:   strings.TrimRight(strings.TrimSpace(rc.DestinationRegistry), "/"),
			Repository: strings.Trim(strings.TrimSpace(rc.DestinationRepo), "/"),
		}
		r.Versions = rc.Versions
		rc.applyTLSOptions(&r.Source, &r.Destination)

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

// applyTLSOptions copies the insecure/CA-cert TLS overrides onto the source
// and destination endpoints. Only image and Helm resources go over a
// registry.SystemContext-backed transport that honors these; git resources
// ignore them, so callers only invoke this for image and helm.
func (rc *ResourceConfig) applyTLSOptions(source, destination *domain.Endpoint) {
	source.Insecure = rc.SourceInsecure
	destination.Insecure = rc.DestinationInsecure
	source.CACertPath = strings.TrimSpace(rc.SourceCACert)
	destination.CACertPath = strings.TrimSpace(rc.DestinationCACert)
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
	firstPart, rest, found := strings.Cut(ref, "/")
	if !found {
		return domain.Endpoint{Repository: ref}
	}

	if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") {
		return domain.Endpoint{
			Registry:   firstPart,
			Repository: rest,
		}
	}

	// No dot or colon in the first segment — treat the whole string as a
	// Docker Hub-style short reference (e.g. "library/ubuntu").
	return domain.Endpoint{Repository: ref}
}
