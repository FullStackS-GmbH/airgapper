// Package logging provides structured logging helpers for Universal Airgapper.
// It uses the standard library's log/slog package with JSON output directed to
// stderr, keeping stdout clean for machine-readable sync results.
package logging

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured logger that writes to stderr. The format
// parameter selects between "json" (default) and "text" output. When debug
// is true the log level is set to DEBUG; otherwise it defaults to INFO.
func NewLogger(debug bool, format string) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

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
