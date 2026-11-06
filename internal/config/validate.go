package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// Validate checks the merged configuration for structural correctness. It
// ensures that every resource has the required fields for its type, that push
// modes are valid, and that scanner names are unique.
//
// Every problem found is reported, joined into one error; each is wrapped
// around domain.ErrInvalidConfig so callers can use
// errors.Is(err, domain.ErrInvalidConfig) for matching.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil: %w", domain.ErrInvalidConfig)
	}

	// Validate scanner name uniqueness.
	if err := validateScanners(cfg.Scanners); err != nil {
		return err
	}
	scanners := make(map[string]bool, len(cfg.Scanners))
	for _, s := range cfg.Scanners {
		scanners[strings.TrimSpace(s.Name)] = true
	}

	// Validate each resource, collecting every problem rather than stopping at
	// the first so one run shows everything that needs fixing.
	var errs []error
	for i, rc := range cfg.Resources {
		if err := validateResource(i, &rc); err != nil {
			errs = append(errs, err)
		} else if ref := rc.ScannerRef; ref != "" && !scanners[ref] {
			errs = append(errs, fmt.Errorf("resource[%d]: scanner_ref %q matches no scanner: %w", i, ref, domain.ErrInvalidConfig))
		}
	}

	return errors.Join(errs...)
}

// validateScanners ensures that all scanner names are unique and non-empty.
func validateScanners(scanners []ScannerConfig) error {
	seen := make(map[string]bool, len(scanners))
	for i, s := range scanners {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return fmt.Errorf("scanner[%d]: name is required: %w", i, domain.ErrInvalidConfig)
		}
		if seen[name] {
			return fmt.Errorf("scanner[%d]: duplicate name %q: %w", i, name, domain.ErrInvalidConfig)
		}
		seen[name] = true
	}
	return nil
}

// validateResource checks that a single ResourceConfig has the required fields
// for its declared type.
func validateResource(index int, rc *ResourceConfig) error {
	prefix := fmt.Sprintf("resource[%d]", index)

	resType := strings.ToLower(strings.TrimSpace(rc.Type))
	if resType == "" {
		return fmt.Errorf("%s: type is required: %w", prefix, domain.ErrInvalidConfig)
	}

	// Normalise "docker" to "image" for validation purposes.
	if resType == "docker" {
		resType = "image"
	}

	switch resType {
	case "image":
		if err := requireNonEmpty(prefix, map[string]string{
			"source":      rc.Source,
			"destination": rc.Destination,
		}); err != nil {
			return err
		}
		if len(rc.Tags) == 0 {
			return fmt.Errorf("%s: at least one tag is required for image resources: %w", prefix, domain.ErrInvalidConfig)
		}
		if strings.TrimSpace(rc.DestinationChart) != "" {
			return fmt.Errorf("%s: destination_chart applies to helm resources only: %w", prefix, domain.ErrInvalidConfig)
		}
		if err := validatePlatforms(prefix, rc.Platforms); err != nil {
			return err
		}

	case "helm":
		if err := requireNonEmpty(prefix, map[string]string{
			"source_registry":      rc.SourceRegistry,
			"source_chart":         rc.SourceChart,
			"destination_registry": rc.DestinationRegistry,
			"destination_repo":     rc.DestinationRepo,
		}); err != nil {
			return err
		}
		if len(rc.Versions) == 0 {
			return fmt.Errorf("%s: at least one version is required for helm resources: %w", prefix, domain.ErrInvalidConfig)
		}
		if strings.TrimSpace(rc.PolicyPath) != "" {
			return fmt.Errorf("%s: policy_path applies to image resources only, charts are not signature-checked: %w", prefix, domain.ErrInvalidConfig)
		}
		if len(rc.Platforms) != 0 {
			return fmt.Errorf("%s: platforms applies to image resources only: %w", prefix, domain.ErrInvalidConfig)
		}
		if name := strings.TrimSpace(rc.DestinationChart); name != "" && strings.Contains(strings.Trim(name, "/"), "/") {
			return fmt.Errorf("%s: destination_chart must be a chart name, not a repository path: %w", prefix, domain.ErrInvalidConfig)
		}

	case "file", "generic":
		for _, raw := range []string{rc.Source, rc.Destination} {
			u, err := url.Parse(strings.TrimSpace(raw))
			if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || strings.HasSuffix(u.Path, "/") {
				return fmt.Errorf("%s: file source and destination must be full HTTP(S) file URLs without userinfo, query or fragment: %w", prefix, domain.ErrInvalidConfig)
			}
		}
		if digest := strings.TrimSpace(rc.SHA256); digest != "" {
			if decoded, err := hex.DecodeString(digest); err != nil || len(decoded) != 32 {
				return fmt.Errorf("%s: sha256 must contain 64 hexadecimal characters: %w", prefix, domain.ErrInvalidConfig)
			}
		}
		if len(rc.Tags)+len(rc.Versions)+len(rc.Refs)+len(rc.Platforms) != 0 || rc.ScannerRef != "" || rc.PolicyPath != "" || rc.DestinationChart != "" {
			return fmt.Errorf("%s: files use exact URLs; tags, versions, refs, scanners and image/chart options do not apply: %w", prefix, domain.ErrInvalidConfig)
		}

	case "git":
		if err := requireNonEmpty(prefix, map[string]string{
			"source_repo":      rc.SourceRepo,
			"destination_repo": rc.DestinationRepo,
		}); err != nil {
			return err
		}
		if len(rc.Refs) == 0 {
			return fmt.Errorf("%s: at least one ref is required for git resources: %w", prefix, domain.ErrInvalidConfig)
		}
		if strings.TrimSpace(rc.PolicyPath) != "" || strings.TrimSpace(rc.DestinationChart) != "" || len(rc.Platforms) != 0 {
			return fmt.Errorf("%s: policy_path, destination_chart and platforms do not apply to git resources: %w", prefix, domain.ErrInvalidConfig)
		}
		if rc.SourceInsecure || rc.DestinationInsecure || strings.TrimSpace(rc.SourceCACert) != "" || strings.TrimSpace(rc.DestinationCACert) != "" {
			return fmt.Errorf("%s: the insecure and ca_cert options do not apply to git resources: %w", prefix, domain.ErrInvalidConfig)
		}

	default:
		return fmt.Errorf("%s: unknown resource type %q: %w", prefix, rc.Type, domain.ErrInvalidConfig)
	}

	if resType != "file" && resType != "generic" && rc.SHA256 != "" {
		return fmt.Errorf("%s: sha256 applies to file resources only: %w", prefix, domain.ErrInvalidConfig)
	}

	// Validate push_mode if explicitly set.
	if _, ok := domain.ParsePushMode(rc.PushMode); !ok {
		return fmt.Errorf("%s: invalid push_mode %q: %w", prefix, rc.PushMode, domain.ErrInvalidConfig)
	}

	return nil
}

// validatePlatforms checks that every platforms entry is "os/architecture" or
// "all", and that "all" is not combined with specific platforms.
func validatePlatforms(prefix string, entries []string) error {
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry), AllPlatforms) {
			if len(entries) > 1 {
				return fmt.Errorf("%s: platforms entry %q cannot be combined with other platforms: %w", prefix, AllPlatforms, domain.ErrInvalidConfig)
			}
			continue
		}
		if _, ok := parsePlatform(entry); !ok {
			return fmt.Errorf("%s: invalid platforms entry %q, want os/architecture (e.g. linux/amd64) or %q: %w", prefix, entry, AllPlatforms, domain.ErrInvalidConfig)
		}
	}
	return nil
}

// requireNonEmpty returns a validation error if any of the named fields have an
// empty (whitespace-only) value.
func requireNonEmpty(prefix string, fields map[string]string) error {
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s: %s is required: %w", prefix, name, domain.ErrInvalidConfig)
		}
	}
	return nil
}
