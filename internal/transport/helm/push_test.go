package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"

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
			defer mu.Unlock()
			require.True(t, tagged, "the version tag must actually be written")
			var manifest ocispec.Manifest
			require.NoError(t, json.Unmarshal(stored, &manifest))
			assert.Equal(t, 2, manifest.SchemaVersion)
			assert.Equal(t, ocispec.MediaTypeImageManifest, manifest.MediaType)
			assert.Equal(t, registry.ConfigMediaType, manifest.Config.MediaType)
			require.Len(t, manifest.Layers, 1)
			assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType)
			assert.Equal(t, digest.FromBytes(data), manifest.Layers[0].Digest)
		})
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
