package helm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestLegacyListVersionsAndExists(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `apiVersion: v1
entries:
  neuvector-crd:
    - version: 108.0.1+up2.8.10
      urls:
        - charts/neuvector-crd-108.0.1+up2.8.10.tgz
    - version: 108.0.0+up2.8.9
      urls:
        - charts/neuvector-crd-108.0.0+up2.8.9.tgz
`)
	}))
	t.Cleanup(srv.Close)

	transporter := New(discardTestLogger())
	endpoint := domain.Endpoint{Registry: srv.URL, Repository: "neuvector-crd"}

	versions, err := transporter.ListVersions(context.Background(), endpoint, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"108.0.1+up2.8.10", "108.0.0+up2.8.9"}, versions)

	exists, err := transporter.Exists(context.Background(), endpoint, "108.0.1+up2.8.10", nil)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = transporter.Exists(context.Background(), endpoint, "108.0.2+up2.8.11", nil)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPullLegacyChartResolvesRelativeURL(t *testing.T) {
	t.Parallel()

	archive := testChartArchive(t, "neuvector-crd", "108.0.1+up2.8.10")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprint(w, `apiVersion: v1
entries:
  neuvector-crd:
    - version: 108.0.1+up2.8.10
      urls:
        - charts/neuvector-crd-108.0.1+up2.8.10.tgz
`)
		case "/charts/neuvector-crd-108.0.1+up2.8.10.tgz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	transporter := New(discardTestLogger())
	endpoint := domain.Endpoint{Registry: srv.URL, Repository: "neuvector-crd"}

	got, sourceURL, err := transporter.pullLegacyChart(context.Background(), endpoint, "108.0.1+up2.8.10", nil)
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/charts/neuvector-crd-108.0.1+up2.8.10.tgz", sourceURL)
	assert.Equal(t, archive, got)
}

func TestDestinationChartRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		destinationRepo string
		sourceChart     string
		want            string
	}{
		{
			name:            "append chart name to base repository",
			destinationRepo: "platform-charts",
			sourceChart:     "neuvector-crd",
			want:            "platform-charts/neuvector-crd",
		},
		{
			name:            "do not duplicate chart name",
			destinationRepo: "platform-charts/neuvector-crd",
			sourceChart:     "neuvector-crd",
			want:            "platform-charts/neuvector-crd",
		},
		{
			name:            "source chart with namespace",
			destinationRepo: "charts",
			sourceChart:     "bitnamicharts/postgresql",
			want:            "charts/postgresql",
		},
		{
			name:            "empty destination repo",
			destinationRepo: "",
			sourceChart:     "neuvector-crd",
			want:            "neuvector-crd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, destinationChartRepository(tt.destinationRepo, tt.sourceChart, ""))
		})
	}
}

func TestDestinationChartRepositoryOverride(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "platform-charts/suse-private-registry", destinationChartRepository(
		"platform-charts",
		"private-registry/1.2/private-registry-helm",
		"suse-private-registry",
	))
}

func TestRenameChartArchive(t *testing.T) {
	t.Parallel()

	archive := testChartArchive(t, "private-registry-helm", "1.2.1")
	renamed, name, err := prepareChartArchive(archive, "suse-private-registry")
	require.NoError(t, err)
	assert.Equal(t, "suse-private-registry", name)

	chart, err := helmloader.LoadArchive(bytes.NewReader(renamed))
	require.NoError(t, err)
	assert.Equal(t, "suse-private-registry", chart.Metadata.Name)
	assert.Equal(t, "1.2.1", chart.Metadata.Version)

	gzipReader, err := gzip.NewReader(bytes.NewReader(renamed))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gzipReader.Close()) })
	header, err := tar.NewReader(gzipReader).Next()
	require.NoError(t, err)
	assert.Equal(t, "suse-private-registry/Chart.yaml", header.Name)
}

func TestPrepareChartArchiveUsesMetadataNameByDefault(t *testing.T) {
	t.Parallel()

	archive := testChartArchiveWithRoot(t, "private-registry-helm", "suse-private-registry", "1.2.1")
	prepared, name, err := prepareChartArchive(archive, "")
	require.NoError(t, err)
	assert.Equal(t, "suse-private-registry", name)

	gzipReader, err := gzip.NewReader(bytes.NewReader(prepared))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gzipReader.Close()) })
	header, err := tar.NewReader(gzipReader).Next()
	require.NoError(t, err)
	assert.Equal(t, "suse-private-registry/Chart.yaml", header.Name)
}

func testChartArchive(t *testing.T, name, version string) []byte {
	t.Helper()
	return testChartArchiveWithRoot(t, name, name, version)
}

func testChartArchiveWithRoot(t *testing.T, root, name, version string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)

	chartYAML := fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", name, version)
	err := tarWriter.WriteHeader(&tar.Header{
		Name: fmt.Sprintf("%s/Chart.yaml", root),
		Mode: 0o644,
		Size: int64(len(chartYAML)),
	})
	require.NoError(t, err)
	_, err = tarWriter.Write([]byte(chartYAML))
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return buf.Bytes()
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testDiscardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type testDiscardWriter struct{}

func (testDiscardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
