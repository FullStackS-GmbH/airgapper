package helmimages_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/helmimages"
)

func TestParseImageRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantReg  string
		wantRepo string
		wantTag  string
	}{
		{"docker hub short name", "nginx", "docker.io", "library/nginx", "latest"},
		{"docker hub namespaced", "bitnami/nginx:1.27.3-debian-12-r0", "docker.io", "bitnami/nginx", "1.27.3-debian-12-r0"},
		{"full ref with host", "ghcr.io/org/app:v1.2.3", "ghcr.io", "org/app", "v1.2.3"},
		{"explicit docker.io prefix", "docker.io/bitnami/nginx:1.27.3", "docker.io", "bitnami/nginx", "1.27.3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, repo, tag := helmimages.ParseImageRef(tc.input)
			assert.Equal(t, tc.wantReg, reg)
			assert.Equal(t, tc.wantRepo, repo)
			assert.Equal(t, tc.wantTag, tag)
		})
	}
}

func TestExtract_RecordsSkippedOnPullFailure(t *testing.T) {
	t.Parallel()

	// A legacy (non-OCI) repo whose index.yaml 404s: the chart pull fails, so
	// the version must be reported as skipped rather than silently dropped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	res := domain.Resource{
		Type:     domain.ResourceTypeHelm,
		Source:   domain.Endpoint{Registry: srv.URL, Repository: "mychart"},
		Versions: []string{"1.0.0"},
	}

	ex := helmimages.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	entries, skipped, err := ex.Extract(context.Background(), []domain.Resource{res}, nil)
	require.NoError(t, err)

	assert.Empty(t, entries)
	require.Len(t, skipped, 1)
	assert.Equal(t, "mychart", skipped[0].Chart)
	assert.Equal(t, "1.0.0", skipped[0].Version)
	assert.Contains(t, skipped[0].Reason, "pull failed")
}

func TestExtractImagesFromYAML_Deployment(t *testing.T) {
	content := `
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: nginx
          image: bitnami/nginx:1.27.3
      initContainers:
        - name: git
          image: bitnami/git:2.47.2
`
	imgs := helmimages.ExtractImagesFromYAML(content)
	assert.ElementsMatch(t, []string{"bitnami/nginx:1.27.3", "bitnami/git:2.47.2"}, imgs)
}

func TestExtractImagesFromYAML_MultiDoc(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"image: app:v1",
		"---",
		"image: sidecar:v2",
	}, "\n")
	imgs := helmimages.ExtractImagesFromYAML(content)
	assert.ElementsMatch(t, []string{"app:v1", "sidecar:v2"}, imgs)
}

func TestExtractImagesFromYAML_Empty(t *testing.T) {
	imgs := helmimages.ExtractImagesFromYAML("")
	assert.Empty(t, imgs)
}

func TestExtractImagesFromYAML_InvalidYAML(t *testing.T) {
	imgs := helmimages.ExtractImagesFromYAML(": invalid: {{.Values.foo}}")
	assert.Empty(t, imgs)
}

func TestBuildOutputYAML_Shape(t *testing.T) {
	entries := []helmimages.ImageEntry{
		{
			Registry:   "docker.io",
			Repository: "bitnami/nginx",
			Tags: []helmimages.TagEntry{
				{Tag: "1.27.3-debian-12-r0", Sources: []string{"bitnamicharts/nginx:18.3.5"}},
				{Tag: "1.27.2-debian-12-r0", Sources: []string{"bitnamicharts/nginx:18.3.4"}},
			},
		},
		{
			Registry:   "docker.io",
			Repository: "bitnami/git",
			Tags: []helmimages.TagEntry{
				{Tag: "2.47.2-debian-12-r0", Sources: []string{"bitnamicharts/nginx:18.3.5", "bitnamicharts/redis:20.11.3"}},
			},
		},
	}

	out, err := helmimages.BuildOutputYAML(entries, "registry.internal.corp", "helm-reg")
	require.NoError(t, err)

	outStr := string(out)
	assert.Contains(t, outStr, "type: image")
	assert.Contains(t, outStr, "source: docker.io/bitnami/nginx")
	assert.Contains(t, outStr, "destination: registry.internal.corp/bitnami/nginx")
	assert.Contains(t, outStr, "target_credentials_ref: helm-reg")
	assert.Contains(t, outStr, `"1.27.3-debian-12-r0"`)
	assert.Contains(t, outStr, "# from helm: bitnamicharts/nginx:18.3.5")
	assert.Contains(t, outStr, "# from helm: bitnamicharts/nginx:18.3.5, bitnamicharts/redis:20.11.3")
}

func TestBuildOutputYAML_SourcesDeduplicatedWithinChartVersion(t *testing.T) {
	// Same tag from the same chart:version (multiple templates) must not
	// produce duplicate source labels.
	entries := []helmimages.ImageEntry{
		{
			Registry:   "docker.io",
			Repository: "bitnami/nginx",
			Tags: []helmimages.TagEntry{
				{Tag: "1.27.3", Sources: []string{"bitnamicharts/nginx:18.3.5"}},
			},
		},
	}
	out, err := helmimages.BuildOutputYAML(entries, "reg.local", "my-cred")
	require.NoError(t, err)
	outStr := string(out)
	// Source label must appear exactly once, not duplicated.
	assert.Equal(t, 1, strings.Count(outStr, "bitnamicharts/nginx:18.3.5"))
}

func TestBuildOutputYAML_DestinationDerivation(t *testing.T) {
	entries := []helmimages.ImageEntry{
		{
			Registry:   "docker.io",
			Repository: "bitnami/nginx",
			Tags:       []helmimages.TagEntry{{Tag: "1.27.3", Sources: []string{"chart:1.0.0"}}},
		},
	}
	out, err := helmimages.BuildOutputYAML(entries, "registry.internal.corp", "helm-reg")
	require.NoError(t, err)
	outStr := string(out)
	assert.Contains(t, outStr, "source: docker.io/bitnami/nginx")
	assert.Contains(t, outStr, "destination: registry.internal.corp/bitnami/nginx")
}
