package helmimages_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/helmimages"
)

func TestRegressionExtractionReportsOmittedImages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		manifest   string
		incomplete bool
	}{
		{name: "valid manifests", manifest: "image: nginx:1.27\n---\nimage: busybox:1.36\n"},
		{name: "digest pinned image", manifest: "image: nginx:1.27\n---\nimage: busybox@sha256:0123456789012345678901234567890123456789012345678901234567890123\n", incomplete: true},
		{name: "malformed later document", manifest: "image: nginx:1.27\n---\nimage: [unterminated\n", incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			archive := extractionRegressionChart(t, tc.manifest)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/index.yaml" {
					_, _ = fmt.Fprint(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [/probe.tgz]\n")
					return
				}
				_, _ = w.Write(archive)
			}))
			t.Cleanup(server.Close)
			resource := domain.Resource{Type: domain.ResourceTypeHelm,
				Source: domain.Endpoint{Registry: server.URL, Repository: "probe"}, Versions: []string{"1.0.0"}}
			extractor := helmimages.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
			entries, skipped, err := extractor.Extract(context.Background(), []domain.Resource{resource}, nil)
			if tc.incomplete {
				// Either a fatal error or a skipped chart makes incompleteness visible to the CLI.
				assert.True(t, err != nil || len(skipped) > 0, "omitted images must not produce a successful complete extraction")
				for _, version := range skipped {
					assert.Equal(t, "probe", version.Chart)
					assert.Equal(t, "1.0.0", version.Version)
					assert.NotEmpty(t, version.Reason)
				}
				return
			}
			require.NoError(t, err)
			assert.Empty(t, skipped)
			require.Len(t, entries, 2)
		})
	}
}

func extractionRegressionChart(t *testing.T, manifest string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, file := range []struct{ name, content string }{
		{"probe/Chart.yaml", "apiVersion: v2\nname: probe\nversion: 1.0.0\n"},
		{"probe/templates/pod.yaml", manifest},
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o600, Size: int64(len(file.content))}))
		_, err := tw.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return archive.Bytes()
}
