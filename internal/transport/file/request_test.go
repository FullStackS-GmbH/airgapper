package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	syncpkg "github.com/fullstacks-gmbh/airgapper/internal/sync"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/retry"
)

func TestSignedRedirectErrorsDoNotLeakIntoReports(t *testing.T) {
	t.Parallel()
	for _, location := range []string{
		"https://cdn.example/invalid%zz?token=REVIEW_DUMMY_TOKEN",
		"https://cdn.example:bad-port/file?token=REVIEW_DUMMY_TOKEN",
	} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusFound)
			}))
			defer server.Close()
			resource := (&config.ResourceConfig{Type: "file", Source: server.URL + "/source", Destination: server.URL + "/destination"}).ToResource()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			engine := syncpkg.NewEngine([]domain.Transporter{New(logger).WithRetry(retry.Policy{Attempts: 2})}, nil, logger)
			results, err := engine.Run(context.Background(), []domain.Resource{resource}, domain.SyncOptions{})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Len(t, results[0].Failed, 1)
			failure := results[0].Failed[0]
			assert.ErrorContains(t, failure.Error, "file HTTP request failed")
			assert.False(t, retry.IsTransient(failure.Error), "malformed redirect is terminal")
			logger.Error("transfer failed", "error", failure.Error)

			text := syncpkg.FormatResult(resource.Type, resource.Source.String(), resource.Destination.String(), failure)
			report, err := json.Marshal(syncpkg.BuildReport(results))
			require.NoError(t, err)
			operations, err := json.Marshal(results[0].Operations)
			require.NoError(t, err)
			path, err := syncpkg.WriteDryRunLog(filepath.Join(t.TempDir(), "report.log"), results, syncpkg.Summarize(results))
			require.NoError(t, err)
			logFile, err := os.ReadFile(path)
			require.NoError(t, err)
			for name, output := range map[string]string{
				"error": failure.Error.Error(), "text": text, "JSON": string(report),
				"operations": string(operations), "report file": string(logFile), "logs": logs.String(),
			} {
				assert.NotContains(t, output, "REVIEW_DUMMY_TOKEN", name)
				assert.NotContains(t, output, "token=", name)
				assert.NotContains(t, output, "cdn.example", name)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequestErrorsPreserveRetryClassificationWithoutLoggingURLs(t *testing.T) {
	t.Parallel()
	const signedURL = "https://cdn.example/file?token=REVIEW_DUMMY_TOKEN"
	for _, tc := range []struct {
		name      string
		cause     error
		transient bool
	}{
		{name: "cancelled", cause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, transient: true},
		{name: "connection reset", cause: syscall.ECONNRESET, transient: true},
		{name: "connection refused", cause: syscall.ECONNREFUSED, transient: true},
		{name: "truncated response", cause: io.ErrUnexpectedEOF, transient: true},
		{name: "timeout", cause: &net.DNSError{Err: signedURL, IsTimeout: true}, transient: true},
		{name: "HTTP unavailable", cause: statusError(http.StatusServiceUnavailable), transient: true},
		{name: "HTTP unauthorized", cause: statusError(http.StatusUnauthorized)},
		{name: "terminal redirect parse", cause: fmt.Errorf("invalid redirect %s", signedURL)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			attempts := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				return nil, &url.Error{Op: "Get", URL: signedURL, Err: tc.cause}
			})}
			err := (retry.Policy{Attempts: 2}).Do(context.Background(), logger, "download file", func() error {
				_, err := request(context.Background(), client, http.MethodGet, "https://source.example/file", nil, 0, nil, "")
				return err
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.cause)
			assert.Equal(t, tc.transient, retry.IsTransient(err))
			if tc.transient {
				assert.Equal(t, 2, attempts)
				assert.Contains(t, logs.String(), "transient failure, retrying")
			} else {
				assert.Equal(t, 1, attempts)
			}
			if tc.name == "timeout" {
				var timeout net.Error
				require.ErrorAs(t, err, &timeout)
				assert.True(t, timeout.Timeout())
			}
			var urlErr *url.Error
			assert.True(t, errors.As(err, &urlErr), "original typed cause remains inspectable")
			assert.NotContains(t, err.Error(), "REVIEW_DUMMY_TOKEN")
			assert.NotContains(t, logs.String(), "REVIEW_DUMMY_TOKEN")
			assert.NotContains(t, logs.String(), "token=")
		})
	}
}
