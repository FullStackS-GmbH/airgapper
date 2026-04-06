package sync

import (
	"fmt"
	"strings"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

// Summary aggregates counts across multiple SyncResults, providing a
// high-level overview of the sync run outcome.
type Summary struct {
	// TotalResources is the number of resources processed.
	TotalResources int

	// TotalVersions is the total number of versions across all resources.
	TotalVersions int

	// Synced is the number of versions successfully copied.
	Synced int

	// Skipped is the number of versions skipped (already present or dry-run).
	Skipped int

	// Failed is the number of versions that failed to sync.
	Failed int
}

// Summarize calculates aggregate counts from a slice of SyncResults.
func Summarize(results []domain.SyncResult) Summary {
	s := Summary{
		TotalResources: len(results),
	}
	for _, r := range results {
		s.TotalVersions += r.TotalCount()
		s.Synced += len(r.Synced)
		s.Skipped += len(r.Skipped)
		s.Failed += len(r.Failed)
	}
	return s
}

// FormatResult returns a human-readable line for a single version result.
// The format is: "[type] source:version -> destination:version  STATUS (message)"
func FormatResult(resourceType domain.ResourceType, source, destination string, vr domain.VersionResult) string {
	status := strings.ToUpper(vr.Status.String())

	line := fmt.Sprintf("[%s] %s:%s -> %s:%s  %s",
		resourceType,
		source, vr.Version,
		destination, vr.Version,
		status,
	)

	if vr.Message != "" {
		line += fmt.Sprintf(" (%s)", vr.Message)
	}

	return line
}
