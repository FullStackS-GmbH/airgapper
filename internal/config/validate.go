package config

import (
	"fmt"
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// Validate checks the merged configuration for structural correctness. It
// ensures that every resource has the required fields for its type, that push
// modes are valid, and that scanner names are unique.
//
// On the first validation error the function returns a descriptive message
// wrapped around domain.ErrInvalidConfig. This allows callers to use
// errors.Is(err, domain.ErrInvalidConfig) for matching.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil: %w", domain.ErrInvalidConfig)
	}

	// Validate scanner name uniqueness.
	if err := validateScanners(cfg.Scanners); err != nil {
		return err
	}

	// Validate each resource.
	for i, rc := range cfg.Resources {
		if err := validateResource(i, &rc); err != nil {
			return err
		}
	}

	return nil
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
		if name := strings.TrimSpace(rc.DestinationChart); name != "" && strings.Contains(strings.Trim(name, "/"), "/") {
			return fmt.Errorf("%s: destination_chart must be a chart name, not a repository path: %w", prefix, domain.ErrInvalidConfig)
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

	default:
		return fmt.Errorf("%s: unknown resource type %q: %w", prefix, rc.Type, domain.ErrInvalidConfig)
	}

	// Validate push_mode if explicitly set.
	if rc.PushMode != "" {
		pm := domain.PushMode(strings.ToLower(strings.TrimSpace(rc.PushMode)))
		if !pm.Valid() {
			return fmt.Errorf("%s: invalid push_mode %q: %w", prefix, rc.PushMode, domain.ErrInvalidConfig)
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
