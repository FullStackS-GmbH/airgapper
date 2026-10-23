package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"

	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestPushChartArtifactoryManifestCompatibility(t *testing.T) {
	t.Parallel()
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated=%t", authenticated), func(t *testing.T) {
			t.Parallel()
			data := testChartArchive(t, "demo", "1.2.3")
			var mu sync.Mutex
			var stored []byte
			blobs := make(map[digest.Digest][]byte)
			var tagged bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if authenticated {
					user, password, ok := r.BasicAuth()
					if !ok || user != "svc-test" || password != "test-token" {
						w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
				}
				switch {
				case r.Method == http.MethodHead:
					w.WriteHeader(http.StatusNotFound)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
					w.Header().Set("Location", r.URL.Path+"upload")
					w.WriteHeader(http.StatusAccepted)
				case r.Method == http.MethodPut:
					body, err := io.ReadAll(r.Body)
					if !assert.NoError(t, err) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if strings.Contains(r.URL.Path, "/manifests/") {
						stored = body
						if strings.HasSuffix(r.URL.Path, "/1.2.3") {
							tagged = true
							assert.Equal(t, ocispec.MediaTypeImageManifest, r.Header.Get("Content-Type"))
						}
					} else {
						blobs[digest.FromBytes(body)] = body
					}
					w.Header().Set("Docker-Content-Digest", digest.FromBytes(body).String())
					w.WriteHeader(http.StatusCreated)
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/manifests/"):
					// Reproduce Artifactory's response for a manifest lacking mediaType.
					w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v1+prettyjws")
					w.Header().Set("Docker-Content-Digest", digest.FromBytes(stored).String())
					_, err := w.Write(stored)
					assert.NoError(t, err)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			t.Cleanup(server.Close)
			destination := domain.Endpoint{Registry: "oci://" + strings.TrimPrefix(server.URL, "http://"), Repository: "charts/demo"}
			var credential *domain.Credential
			if authenticated {
				credential = &domain.Credential{Username: "svc-test", Password: "test-token"}
			} else {
				// Prove the existing Helm SDK reproduces the observed failure.
				client, err := newRegistryClient(context.Background(), domain.Endpoint{}, destination)
				require.NoError(t, err)
				_, err = client.Push(data, NormalizeOCIRef(destination.Registry, destination.Repository, "1.2.3"))
				require.ErrorContains(t, err, "mismatch response Content-Type")
			}
			require.NoError(t, pushChart(context.Background(), data, destination, "1.2.3", credential))
			mu.Lock()
			var directManifest ocispec.Manifest
			decodeErr := json.Unmarshal(stored, &directManifest)
			mu.Unlock()
			require.NoError(t, decodeErr)
			require.Len(t, directManifest.Layers, 1)
			assert.Equal(t, digest.FromBytes(data), directManifest.Layers[0].Digest)

			// Exercise the production sync path too, including the early
			// destination check and a legacy HTTP chart download.
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("Authorization"), "destination credentials must not reach the source")
				if r.URL.Path == "/index.yaml" {
					_, err := io.WriteString(w, "entries:\n  demo:\n  - version: 1.2.3\n    urls: [demo-1.2.3.tgz]\n")
					assert.NoError(t, err)
					return
				}
				_, err := w.Write(data)
				assert.NoError(t, err)
			}))
			t.Cleanup(source.Close)
			resource := domain.Resource{
				Type:        domain.ResourceTypeHelm,
				Source:      domain.Endpoint{Registry: source.URL, Repository: "demo"},
				Destination: domain.Endpoint{Registry: destination.Registry, Repository: "charts"},
				Versions:    []string{"1.2.3"},
				PushMode:    domain.PushModeForce,
			}
			var credentialStore domain.CredentialStore
			if credential != nil {
				entry := *credential
				entry.Name = "destination"
				entry.Type = domain.CredentialTypeHelm
				credentialStore = credentials.NewFileStore([]domain.Credential{entry})
				resource.TargetCredentialsRef = entry.Name
			}
			mu.Lock()
			tagged = false
			mu.Unlock()
			result, err := New(discardTestLogger()).Sync(context.Background(), resource, domain.SyncOptions{Credentials: credentialStore})
			require.NoError(t, err)
			require.Empty(t, result.Failed)
			require.Len(t, result.Synced, 1)
			mu.Lock()
			defer mu.Unlock()
			require.True(t, tagged, "the version tag must actually be written")
			var manifest ocispec.Manifest
			require.NoError(t, json.Unmarshal(stored, &manifest))
			assert.Equal(t, 2, manifest.SchemaVersion)
			assert.Equal(t, ocispec.MediaTypeImageManifest, manifest.MediaType)
			assert.Equal(t, registry.ConfigMediaType, manifest.Config.MediaType)
			require.Len(t, manifest.Layers, 1)
			assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType)
			chart, err := helmloader.LoadArchive(bytes.NewReader(blobs[manifest.Layers[0].Digest]))
			require.NoError(t, err)
			assert.Equal(t, "demo", chart.Metadata.Name)
			assert.Equal(t, "1.2.3", chart.Metadata.Version)
		})
	}
}

func TestPushChartRetriesTransientRegistryFailures(t *testing.T) {
	t.Parallel()
	data := testChartArchive(t, "demo", "1.2.3")
	var requests atomic.Int32
	var tagged atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			w.Header().Set("Location", r.URL.Path+"upload")
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/manifests/1.2.3") {
				tagged.Store(true)
			}
			w.Header().Set("Docker-Content-Digest", digest.FromBytes(body).String())
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	destination := domain.Endpoint{Registry: server.URL, Repository: "charts/demo"}
	require.NoError(t, pushChart(ctx, data, destination, "1.2.3", nil))
	assert.True(t, tagged.Load(), "a transient registry failure must not prevent the final tag")
}

func TestPushChartStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()
	data := testChartArchive(t, "demo", "1.2.3")
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- pushChart(ctx, data, domain.Endpoint{Registry: server.URL, Repository: "charts/demo"}, "1.2.3", nil)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the server")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("the push kept running after cancellation")
	}
}

func TestPushChartRejectsMismatchedMetadata(t *testing.T) {
	t.Parallel()
	data := testChartArchive(t, "demo", "1.2.3")
	for _, tc := range []struct{ repository, version string }{
		{"charts/other", "1.2.3"},
		{"charts/demo", "2.0.0"},
	} {
		t.Run(tc.repository+":"+tc.version, func(t *testing.T) {
			t.Parallel()
			err := pushChart(context.Background(), data, domain.Endpoint{Registry: "oci://registry.invalid", Repository: tc.repository}, tc.version, nil)
			require.ErrorContains(t, err, "must match Chart.yaml")
		})
	}
}
