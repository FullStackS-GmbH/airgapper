package logging

import (
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoggerMasksURLCredentials(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			output, err := os.CreateTemp(t.TempDir(), "log")
			require.NoError(t, err)
			defer func() { require.NoError(t, output.Close()) }()
			previous := os.Stderr
			os.Stderr = output
			defer func() { os.Stderr = previous }()

			url := "https://user:s3cret@host/repo"
			logger := NewLogger(true, format).With("source", url).WithGroup("transport")
			logger.Error("request failed: "+url,
				"error", errors.New("fetch "+url),
				"destination", "ssh://token@other/repo",
				slog.Group("details", slog.String("url", url)),
				"count", 42,
			)
			data, err := os.ReadFile(output.Name())
			require.NoError(t, err)
			assert.NotContains(t, string(data), "s3cret")
			assert.NotContains(t, string(data), "user:")
			assert.NotContains(t, string(data), "token@")
			assert.Contains(t, string(data), "https://***@host/repo")
			assert.Contains(t, string(data), "ssh://***@other/repo")
			assert.Contains(t, string(data), "42")
		})
	}
}
