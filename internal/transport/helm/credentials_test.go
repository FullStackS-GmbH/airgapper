package helm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestPullLegacyChartSendsCredentialsOnlyToRepositoryHost(t *testing.T) {
	t.Parallel()

	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth.Store(r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, "chart")
	}))
	defer other.Close()

	var repoChartAuth atomic.Value
	repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, "entries:\n  abs:\n  - version: 1.0.0\n    urls: [%q]\n  rel:\n  - version: 1.0.0\n    urls: [rel-1.0.0.tgz]\n", other.URL+"/abs.tgz")
		default:
			repoChartAuth.Store(r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, "chart")
		}
	}))
	defer repo.Close()

	tr := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	creds := &domain.Credential{Username: "alice", Password: "s3cret"}

	_, _, err := tr.pullLegacyChart(context.Background(), domain.Endpoint{Registry: repo.URL, Repository: "abs"}, "1.0.0", creds)
	require.NoError(t, err)
	assert.Empty(t, otherAuth.Load(), "credentials must not follow a chart URL to another host")

	_, _, err = tr.pullLegacyChart(context.Background(), domain.Endpoint{Registry: repo.URL, Repository: "rel"}, "1.0.0", creds)
	require.NoError(t, err)
	assert.NotEmpty(t, repoChartAuth.Load(), "credentials still apply to the repository's own host")
}

func TestSyncDoesNotDownloadChartWhenDestinationDecides(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		dryRun     bool
		destStatus int
		pushMode   domain.PushMode
		insecure   bool
	}{
		{name: "dry-run, absent", dryRun: true, destStatus: http.StatusNotFound, insecure: true},
		// A loopback registry is plain HTTP without any insecure flag.
		{name: "skip, present, local registry", destStatus: http.StatusOK, pushMode: domain.PushModeSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var downloads atomic.Int32
			repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/index.yaml" {
					_, _ = io.WriteString(w, "entries:\n  mychart:\n  - version: 1.0.0\n    urls: [mychart-1.0.0.tgz]\n")
					return
				}
				downloads.Add(1)
				_, _ = io.WriteString(w, "chart")
			}))
			defer repo.Close()

			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/" {
					return
				}
				if tc.destStatus == http.StatusOK {
					w.Header().Set("Docker-Content-Digest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
				}
				w.WriteHeader(tc.destStatus)
			}))
			defer registry.Close()

			res := domain.Resource{
				Type:        domain.ResourceTypeHelm,
				Source:      domain.Endpoint{Registry: repo.URL, Repository: "mychart"},
				Destination: domain.Endpoint{Registry: registry.Listener.Addr().String(), Repository: "charts", Insecure: tc.insecure},
				Versions:    []string{"1.0.0"},
				PushMode:    tc.pushMode,
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			result, err := New(logger).Sync(context.Background(), res, domain.SyncOptions{DryRun: tc.dryRun, Logger: logger})

			require.NoError(t, err)
			assert.Empty(t, result.Failed)
			assert.Len(t, result.Skipped, 1)
			assert.Zero(t, downloads.Load(), "the chart must not be downloaded")
		})
	}
}
