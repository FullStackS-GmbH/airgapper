package image

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/pkg/sysregistriesv2"

	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestRegressionImageDryRunFailsWhenDestinationCannotBeChecked(t *testing.T) {
	t.Parallel()
	resource := domain.Resource{
		Type:        domain.ResourceTypeImage,
		Source:      domain.Endpoint{Registry: "source.example.com", Repository: "probe"},
		Destination: domain.Endpoint{Registry: "invalid registry", Repository: "probe"},
		Versions:    []string{"1.0.0"}, PushMode: domain.PushModeSkip,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := New(logger).Sync(context.Background(), resource, domain.SyncOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, result.Failed, 1, "an invalid destination must not become a successful dry-run plan")
	assert.Error(t, result.Failed[0].Error)
	assert.Empty(t, result.Skipped)
	assert.Empty(t, result.Synced)
}

func TestRegressionImageDryRunReportsRegistryProbeErrors(t *testing.T) {
	// Environment-based registry configuration requires sequential subtests.
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var probes, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					writes.Add(1)
				}
				if r.URL.Path == "/v2/" {
					w.WriteHeader(http.StatusOK)
					return
				}
				probes.Add(1)
				w.Header().Set("Retry-After", "0")
				w.Header().Set("WWW-Authenticate", `Basic realm="probe"`)
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)
			host := strings.TrimPrefix(server.URL, "http://")
			conf := filepath.Join(t.TempDir(), "registries.conf")
			require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("[[registry]]\nlocation = %q\ninsecure = true\n", host)), 0o600))
			t.Setenv("CONTAINERS_REGISTRIES_CONF", conf)
			sysregistriesv2.InvalidateCache()
			t.Cleanup(sysregistriesv2.InvalidateCache)
			resource := domain.Resource{Type: domain.ResourceTypeImage,
				Source:      domain.Endpoint{Registry: "source.example.com", Repository: "probe"},
				Destination: domain.Endpoint{Registry: host, Repository: "probe"}, Versions: []string{"v1"},
				PushMode: domain.PushModeSkip, TargetCredentialsRef: "target"}
			store := credentials.NewFileStore([]domain.Credential{{Name: "target", Type: domain.CredentialTypeImage, Username: "probe", Password: "dummy"}})
			result, err := New(slog.New(slog.NewTextHandler(io.Discard, nil))).Sync(t.Context(), resource,
				domain.SyncOptions{DryRun: true, Credentials: store})
			require.NoError(t, err)
			assert.Positive(t, probes.Load(), "fixture must receive a destination probe")
			assert.Zero(t, writes.Load(), "dry-run must not write to the registry")
			require.Len(t, result.Failed, 1, "registry probe errors must fail the dry run")
			assert.Error(t, result.Failed[0].Error)
			assert.Empty(t, result.Skipped)
		})
	}
}
