// Package logging provides structured logging helpers for Universal Airgapper.
// It uses the standard library's log/slog package with JSON output directed to
// stderr, keeping stdout clean for machine-readable sync results.
package logging

import (
	"log/slog"
	"os"
)

// NewLogger creates a new structured JSON logger that writes to stderr. When
// debug is true the log level is set to DEBUG; otherwise it defaults to INFO.
func NewLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}

// WithResource returns a child logger enriched with resource context
// attributes. This makes it easy to correlate log entries with the specific
// resource being processed.
func WithResource(logger *slog.Logger, resourceType, source, destination string) *slog.Logger {
	return logger.With(
		slog.String("resource_type", resourceType),
		slog.String("source", source),
		slog.String("destination", destination),
	)
}
