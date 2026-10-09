// Package logging provides structured logging helpers for Universal Airgapper.
// It uses the standard library's log/slog package with JSON output directed to
// stderr, keeping stdout clean for machine-readable sync results.
package logging

import (
	"log/slog"
	"os"

	"github.com/fullstacks-gmbh/airgapper/internal/redact"
)

// NewLogger creates a structured logger that writes to stderr. The format
// parameter selects between "json" (default) and "text" output. When debug
// is true the log level is set to DEBUG; otherwise it defaults to INFO.
func NewLogger(debug bool, format string) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redactAttr}

	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	switch attr.Value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(redact.URLCredentials(attr.Value.String()))
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			attr.Value = slog.StringValue(redact.URLCredentials(err.Error()))
		}
	}
	return attr
}
