package image_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fullstacks-gmbh/airgapper/internal/transport/image"
)

func TestParseImageRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		wantReg  string
		wantRepo string
	}{
		{
			name:     "simple name expands to Docker Hub library",
			ref:      "ubuntu",
			wantReg:  "index.docker.io",
			wantRepo: "library/ubuntu",
		},
		{
			name:     "namespaced expands to Docker Hub",
			ref:      "myrepo/image",
			wantReg:  "index.docker.io",
			wantRepo: "myrepo/image",
		},
		{
			name:     "full registry reference",
			ref:      "registry.example.com/repo/image",
			wantReg:  "registry.example.com",
			wantRepo: "repo/image",
		},
		{
			name:     "ghcr.io reference",
			ref:      "ghcr.io/org/app",
			wantReg:  "ghcr.io",
			wantRepo: "org/app",
		},
		{
			name:     "reference with port",
			ref:      "localhost:5000/myimage",
			wantReg:  "localhost:5000",
			wantRepo: "myimage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, repo := image.ParseImageRef(tt.ref)
			assert.Equal(t, tt.wantReg, reg)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}
