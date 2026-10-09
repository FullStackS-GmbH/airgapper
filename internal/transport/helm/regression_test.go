package helm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/pkg/sysregistriesv2"

	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestRegressionLegacyChartCredentialsStayAtRepositoryOrigin(t *testing.T) {
	t.Parallel()
	for _, sameOrigin := range []bool{true, false} {
		name := "external origin"
		if sameOrigin {
			name = "repository origin"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var indexAuth, chartAuth atomic.Bool
			archive := testChartArchive(t, "probe", "1.0.0")
			chartHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				chartAuth.Store(ok && user == "probe-user" && password == "dummy-password")
				// Detect any Authorization header, even if it is not Basic Auth.
				if !sameOrigin {
					assert.Empty(t, r.Header.Get("Authorization"), "external downloads must not receive repository credentials")
				}
				_, _ = w.Write(archive)
			})
			external := httptest.NewServer(chartHandler)
			t.Cleanup(external.Close)
			repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/index.yaml" {
					chartHandler.ServeHTTP(w, r)
					return
				}
				user, password, ok := r.BasicAuth()
				indexAuth.Store(ok && user == "probe-user" && password == "dummy-password")
				chartURL := external.URL + "/probe.tgz"
				if sameOrigin {
					chartURL = "/probe.tgz"
				}
				_, _ = fmt.Fprintf(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [%q]\n", chartURL)
			}))
			t.Cleanup(repo.Close)
			data, _, err := New(discardTestLogger()).pullLegacyChart(context.Background(),
				domain.Endpoint{Registry: repo.URL, Repository: "probe"}, "1.0.0",
				&domain.Credential{Username: "probe-user", Password: "dummy-password"})
			require.NoError(t, err)
			assert.Equal(t, archive, data)
			assert.True(t, indexAuth.Load(), "index requests still need repository credentials")
			assert.Equal(t, sameOrigin, chartAuth.Load())
		})
	}
}

func TestRegressionHelmDryRunFailsWhenDestinationCannotBeChecked(t *testing.T) {
	t.Parallel()
	archive := testChartArchive(t, "probe", "1.0.0")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = fmt.Fprint(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [/probe.tgz]\n")
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(source.Close)
	resource := domain.Resource{
		Type:        domain.ResourceTypeHelm,
		Source:      domain.Endpoint{Registry: source.URL, Repository: "probe"},
		Destination: domain.Endpoint{Registry: "oci://invalid registry", Repository: "charts"},
		Versions:    []string{"1.0.0"}, PushMode: domain.PushModeSkip,
	}
	result, err := New(discardTestLogger()).Sync(context.Background(), resource, domain.SyncOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, result.Failed, 1, "an invalid destination must not become a successful dry-run plan")
	assert.Error(t, result.Failed[0].Error)
	assert.Empty(t, result.Skipped)
	assert.Empty(t, result.Synced)
}

func TestRegressionHelmOCIPullHonorsCancellation(t *testing.T) {
	t.Parallel()
	for _, authenticated := range []bool{false, true} {
		name := "anonymous pull"
		if authenticated {
			name = "authenticated login and pull"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			resource := domain.Resource{Type: domain.ResourceTypeHelm,
				Source: domain.Endpoint{Registry: "oci://" + strings.TrimPrefix(server.URL, "http://"), Repository: "probe"}}
			var store domain.CredentialStore
			if authenticated {
				resource.SourceCredentialsRef = "source"
				store = credentials.NewFileStore([]domain.Credential{{Name: "source", Type: domain.CredentialTypeHelm, Username: "probe", Password: "dummy"}})
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := New(discardTestLogger()).PullChartBytes(ctx, resource, "1.0.0", store)
			assert.ErrorIs(t, err, context.Canceled)
			assert.Zero(t, requests.Load(), "a canceled operation must not start OCI requests")
		})
	}
}

func TestRegressionHelmDryRunReportsRegistryProbeErrors(t *testing.T) {
	// Environment-based registry configuration requires sequential subtests.
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			archive := testChartArchive(t, "probe", "1.0.0")
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/index.yaml" {
					_, _ = fmt.Fprint(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [/probe.tgz]\n")
					return
				}
				_, _ = w.Write(archive)
			}))
			t.Cleanup(source.Close)
			var probes, writes atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			t.Cleanup(target.Close)
			host := strings.TrimPrefix(target.URL, "http://")
			conf := filepath.Join(t.TempDir(), "registries.conf")
			require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("[[registry]]\nlocation = %q\ninsecure = true\n", host)), 0o600))
			t.Setenv("CONTAINERS_REGISTRIES_CONF", conf)
			sysregistriesv2.InvalidateCache()
			t.Cleanup(sysregistriesv2.InvalidateCache)
			resource := domain.Resource{Type: domain.ResourceTypeHelm,
				Source:      domain.Endpoint{Registry: source.URL, Repository: "probe"},
				Destination: domain.Endpoint{Registry: "oci://" + host, Repository: "charts"}, Versions: []string{"1.0.0"},
				PushMode: domain.PushModeSkip, TargetCredentialsRef: "target"}
			store := credentials.NewFileStore([]domain.Credential{{Name: "target", Type: domain.CredentialTypeHelm, Username: "probe", Password: "dummy"}})
			result, err := New(discardTestLogger()).Sync(t.Context(), resource, domain.SyncOptions{DryRun: true, Credentials: store})
			require.NoError(t, err)
			assert.Positive(t, probes.Load(), "fixture must receive a destination probe")
			assert.Zero(t, writes.Load(), "dry-run must not write to the registry")
			require.Len(t, result.Failed, 1, "registry probe errors must fail the dry run")
			assert.Error(t, result.Failed[0].Error)
			assert.Empty(t, result.Skipped)
		})
	}
}

func TestRegressionHelmOCIPullCancelsInFlightRequest(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-release:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(discardTestLogger()).PullChartBytes(ctx, domain.Resource{Type: domain.ResourceTypeHelm,
			Source: domain.Endpoint{Registry: "oci://" + strings.TrimPrefix(server.URL, "http://"), Repository: "probe"}}, "1.0.0", nil)
		done <- err
	}()
	returned := false
	t.Cleanup(func() {
		if !returned {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("OCI request did not finish after fixture release")
			}
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("OCI request never reached the fixture")
	}
	cancel()
	select {
	case err := <-done:
		returned = true
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Error("Helm OCI pull continued after cancellation")
	}
}

func TestRegressionHelmOCIPushCancelsInFlightRequest(t *testing.T) {
	// The test registry is configured as plain HTTP only for this fixture.
	archive := testChartArchive(t, "probe", "1.0.0")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = fmt.Fprint(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [/probe.tgz]\n")
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(source.Close)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost {
			once.Do(func() { close(started) })
			select {
			case <-r.Context().Done():
			case <-release:
				// A non-retryable response allows cleanup even if cancellation is ignored.
				w.WriteHeader(http.StatusBadRequest)
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(target.Close)
	defer close(release)
	host := strings.TrimPrefix(target.URL, "http://")
	conf := filepath.Join(t.TempDir(), "registries.conf")
	require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("[[registry]]\nlocation = %q\ninsecure = true\n", host)), 0o600))
	t.Setenv("CONTAINERS_REGISTRIES_CONF", conf)
	sysregistriesv2.InvalidateCache()
	t.Cleanup(sysregistriesv2.InvalidateCache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := New(discardTestLogger()).Sync(ctx, domain.Resource{Type: domain.ResourceTypeHelm,
			Source:      domain.Endpoint{Registry: source.URL, Repository: "probe"},
			Destination: domain.Endpoint{Registry: "oci://" + host, Repository: "charts"},
			Versions:    []string{"1.0.0"}, PushMode: domain.PushModeForce}, domain.SyncOptions{})
		if err == nil && len(result.Failed) > 0 {
			err = result.Failed[0].Error
		}
		done <- err
	}()
	returned := false
	t.Cleanup(func() {
		if !returned {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("OCI push did not finish after fixture release")
			}
		}
	})
	select {
	case <-started:
	case err := <-done:
		returned = true
		t.Fatalf("sync returned before starting a push: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("OCI push never reached the fixture")
	}
	cancel()
	select {
	case err := <-done:
		returned = true
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Error("Helm OCI push continued after cancellation")
	}
}
