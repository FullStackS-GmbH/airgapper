package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// WriteDryRunLog writes a human-readable dry-run report to the specified path.
// If path is empty, a timestamped file is created in the current working
// directory. The function returns the actual path written.
func WriteDryRunLog(path string, results []domain.SyncResult, summary Summary) (string, error) {
	if path == "" {
		path = fmt.Sprintf("airgapper-dry-run-%s.log", time.Now().Format("20060102-150405"))
	}

	// Ensure parent directory exists.
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory %q: %w", dir, err)
		}
	}

	var b strings.Builder

	// Header.
	fmt.Fprintf(&b, "Universal Airgapper — Dry-Run Report\n")
	fmt.Fprintf(&b, "Generated: %s\n", time.Now().Format(time.RFC3339))
	b.WriteString(strings.Repeat("=", 60) + "\n\n")

	// Per-resource results.
	for _, result := range results {
		source := result.Resource.Source.String()
		dest := result.Resource.Destination.String()

		fmt.Fprintf(&b, "[%s] %s -> %s\n", result.Resource.Type, source, dest)

		all := make([]domain.VersionResult, 0, result.TotalCount())
		all = append(all, result.Synced...)
		all = append(all, result.Skipped...)
		all = append(all, result.Failed...)

		for _, vr := range all {
			b.WriteString("  " + FormatResult(result.Resource.Type, source, dest, vr) + "\n")
		}
		b.WriteString("\n")
	}

	// Operation log.
	b.WriteString(strings.Repeat("-", 60) + "\n")
	b.WriteString("Operation Log\n")
	b.WriteString(strings.Repeat("-", 60) + "\n")

	for _, result := range results {
		for _, op := range result.Operations {
			fmt.Fprintf(&b, "  [%s] %-9s %s", op.ResourceType, op.Operation, op.Version)
			if op.Source != "" {
				fmt.Fprintf(&b, "  %s -> %s", op.Source, op.Destination)
			}
			if op.Message != "" {
				fmt.Fprintf(&b, "  (%s)", op.Message)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Summary.
	b.WriteString(FormatSummary(summary))

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write dry-run log: %w", err)
	}

	return path, nil
}
