package sync

import (
	"fmt"
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// OperationCounts aggregates counts by operation type.
type OperationCounts struct {
	Read      int
	Pull      int
	Push      int
	Skip      int
	Overwrite int
	Force     int
	Fail      int
}

// ResourceTypeSummary provides per-resource-type breakdown.
type ResourceTypeSummary struct {
	Type       domain.ResourceType
	Versions   int
	Synced     int
	Skipped    int
	Failed     int
	Operations OperationCounts
}

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

	// Operations aggregates counts by operation type across all resources.
	Operations OperationCounts

	// ByType provides a per-resource-type breakdown.
	ByType map[domain.ResourceType]ResourceTypeSummary
}

// Summarize calculates aggregate counts from a slice of SyncResults.
func Summarize(results []domain.SyncResult) Summary {
	s := Summary{
		TotalResources: len(results),
		ByType:         make(map[domain.ResourceType]ResourceTypeSummary),
	}

	for _, r := range results {
		s.TotalVersions += r.TotalCount()
		s.Synced += len(r.Synced)
		s.Skipped += len(r.Skipped)
		s.Failed += len(r.Failed)

		// Aggregate operation counts.
		for _, op := range r.Operations {
			addOp(&s.Operations, op.Operation)
		}

		// Per-type breakdown.
		rt := r.Resource.Type
		ts := s.ByType[rt]
		ts.Type = rt
		ts.Versions += r.TotalCount()
		ts.Synced += len(r.Synced)
		ts.Skipped += len(r.Skipped)
		ts.Failed += len(r.Failed)
		for _, op := range r.Operations {
			addOp(&ts.Operations, op.Operation)
		}
		s.ByType[rt] = ts
	}

	return s
}

// addOp increments the appropriate counter in OperationCounts.
func addOp(c *OperationCounts, op domain.OperationType) {
	switch op {
	case domain.OpRead:
		c.Read++
	case domain.OpPull:
		c.Pull++
	case domain.OpPush:
		c.Push++
	case domain.OpSkip:
		c.Skip++
	case domain.OpOverwrite:
		c.Overwrite++
	case domain.OpForce:
		c.Force++
	case domain.OpFail:
		c.Fail++
	}
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

// FormatSummary returns a human-readable multi-line summary of the sync run.
func FormatSummary(s Summary) string {
	var b strings.Builder

	b.WriteString("\n=== Sync Summary ===\n")
	fmt.Fprintf(&b, "Resources: %d | Versions: %d | Synced: %d | Skipped: %d | Failed: %d\n",
		s.TotalResources, s.TotalVersions, s.Synced, s.Skipped, s.Failed)

	fmt.Fprintf(&b, "Operations: read=%d pull=%d push=%d skip=%d overwrite=%d force=%d fail=%d\n",
		s.Operations.Read, s.Operations.Pull, s.Operations.Push,
		s.Operations.Skip, s.Operations.Overwrite, s.Operations.Force, s.Operations.Fail)

	if len(s.ByType) > 0 {
		b.WriteString("\nBy type:\n")
		for _, rt := range []domain.ResourceType{domain.ResourceTypeImage, domain.ResourceTypeHelm, domain.ResourceTypeGit} {
			ts, ok := s.ByType[rt]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  %s: %d versions (%d synced, %d skipped, %d failed)\n",
				rt, ts.Versions, ts.Synced, ts.Skipped, ts.Failed)
		}
	}

	return b.String()
}
