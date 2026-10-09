package image

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/copy"
	ocilayout "go.podman.io/image/v5/oci/layout"
	"golang.org/x/sync/errgroup"
)

func TestCopyImageConcurrentMultiPlatform(t *testing.T) {
	t.Parallel()
	sourceDir := multiPlatformLayout(t)
	source, err := ocilayout.ParseReference(sourceDir + ":test")
	require.NoError(t, err)
	var group errgroup.Group
	for range 4 {
		destination, err := ocilayout.ParseReference(t.TempDir() + ":test")
		require.NoError(t, err)
		group.Go(func() error {
			ctx := context.Background()
			if err := copyImage(ctx, destination, source, &copy.Options{ImageListSelection: copy.CopyAllImages, ReportWriter: io.Discard}); err != nil {
				return err
			}
			copied, err := destination.NewImageSource(ctx, nil)
			if err != nil {
				return err
			}
			defer func() { assert.NoError(t, copied.Close()) }()
			manifest, _, err := copied.GetManifest(ctx, nil)
			if err != nil {
				return err
			}
			var index struct {
				Manifests []struct{ Digest digest.Digest }
			}
			if err := json.Unmarshal(manifest, &index); err != nil {
				return err
			}
			assert.Len(t, index.Manifests, 2, "all platform manifests must be copied")
			for _, child := range index.Manifests {
				if _, _, err := copied.GetManifest(ctx, &child.Digest); err != nil {
					return err
				}
			}
			return nil
		})
	}
	require.NoError(t, group.Wait())
}

// multiPlatformLayout creates a small OCI fixture without external registries.
func multiPlatformLayout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs", "sha256")
	require.NoError(t, os.MkdirAll(blobs, 0o755))
	writeBlob := func(mediaType string, value any) map[string]any {
		t.Helper()
		data, err := json.Marshal(value)
		require.NoError(t, err)
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		require.NoError(t, os.WriteFile(filepath.Join(blobs, hash), data, 0o600))
		return map[string]any{"mediaType": mediaType, "digest": "sha256:" + hash, "size": len(data)}
	}
	var manifests []map[string]any
	for _, arch := range []string{"amd64", "arm64"} {
		config := writeBlob("application/vnd.oci.image.config.v1+json", map[string]any{
			"architecture": arch, "os": "linux", "config": map[string]any{},
			"rootfs": map[string]any{"type": "layers", "diff_ids": []string{}},
		})
		manifest := writeBlob("application/vnd.oci.image.manifest.v1+json", map[string]any{
			"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
			"config": config, "layers": []any{},
		})
		manifest["platform"] = map[string]any{"architecture": arch, "os": "linux"}
		manifests = append(manifests, manifest)
	}
	index := writeBlob("application/vnd.oci.image.index.v1+json", map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": manifests,
	})
	index["annotations"] = map[string]string{"org.opencontainers.image.ref.name": "test"}
	data, err := json.Marshal(map[string]any{"schemaVersion": 2, "manifests": []any{index}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), data, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o600))
	return dir
}
