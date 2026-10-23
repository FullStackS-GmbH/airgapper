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

// chartWithManifest packs a minimal chart whose only template is manifest.
func chartWithManifest(t *testing.T, manifest string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, content string }{
		{"probe/Chart.yaml", "apiVersion: v2\nname: probe\nversion: 1.0.0\n"},
		{"probe/templates/pod.yaml", manifest},
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o600, Size: int64(len(f.content))}))
		_, err := tw.Write([]byte(f.content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return archive.Bytes()
}

func TestExtractReportsOmittedImages(t *testing.T) {
	t.Parallel()
	digest := "busybox@sha256:0123456789012345678901234567890123456789012345678901234567890123"

	for _, tc := range []struct {
		name        string
		manifest    string
		wantEntries int
		wantReason  string
	}{
		{name: "complete", manifest: "image: nginx:1.27\n---\nimage: busybox:1.36\n", wantEntries: 2},
		{name: "digest pinned", manifest: "image: nginx:1.27\n---\nimage: " + digest + "\n", wantEntries: 1, wantReason: "pinned by digest"},
		{name: "malformed later document", manifest: "image: nginx:1.27\n---\nimage: [unterminated\n", wantEntries: 1, wantReason: "parse rendered YAML"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			archive := chartWithManifest(t, tc.manifest)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/index.yaml" {
					_, _ = fmt.Fprint(w, "entries:\n  probe:\n    - version: 1.0.0\n      urls: [/probe.tgz]\n")
					return
				}
				_, _ = w.Write(archive)
			}))
			defer server.Close()

			res := domain.Resource{
				Type:     domain.ResourceTypeHelm,
				Source:   domain.Endpoint{Registry: server.URL, Repository: "probe"},
				Versions: []string{"1.0.0"},
			}
			entries, skipped, err := helmimages.New(slog.New(slog.NewTextHandler(io.Discard, nil))).
				Extract(context.Background(), []domain.Resource{res}, nil)

			require.NoError(t, err)
			assert.Len(t, entries, tc.wantEntries, "images that could be read are still returned")
			if tc.wantReason == "" {
				assert.Empty(t, skipped)
				return
			}
			require.Len(t, skipped, 1)
			assert.Equal(t, "probe", skipped[0].Chart)
			assert.Equal(t, "1.0.0", skipped[0].Version)
			assert.Contains(t, skipped[0].Reason, tc.wantReason)
		})
	}
}
