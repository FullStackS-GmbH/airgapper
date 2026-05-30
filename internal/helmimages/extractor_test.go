package helmimages_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/helmimages"
)

func TestParseImageRef_DockerHubShort(t *testing.T) {
	reg, repo, tag := helmimages.ParseImageRef("nginx")
	assert.Equal(t, "docker.io", reg)
	assert.Equal(t, "library/nginx", repo)
	assert.Equal(t, "latest", tag)
}

func TestParseImageRef_DockerHubNamespaced(t *testing.T) {
	reg, repo, tag := helmimages.ParseImageRef("bitnami/nginx:1.27.3-debian-12-r0")
	assert.Equal(t, "docker.io", reg)
	assert.Equal(t, "bitnami/nginx", repo)
	assert.Equal(t, "1.27.3-debian-12-r0", tag)
}

func TestParseImageRef_FullRef(t *testing.T) {
	reg, repo, tag := helmimages.ParseImageRef("ghcr.io/org/app:v1.2.3")
	assert.Equal(t, "ghcr.io", reg)
	assert.Equal(t, "org/app", repo)
	assert.Equal(t, "v1.2.3", tag)
}

func TestParseImageRef_ExplicitDockerIO(t *testing.T) {
	reg, repo, tag := helmimages.ParseImageRef("docker.io/bitnami/nginx:1.27.3")
	assert.Equal(t, "docker.io", reg)
	assert.Equal(t, "bitnami/nginx", repo)
	assert.Equal(t, "1.27.3", tag)
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
