package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// pushChart writes a Helm OCI manifest with an explicit mediaType. Helm 4.0.4
// omits that field, which causes Artifactory to return the Docker schema-1
// Content-Type on the digest GET made during Helm's subsequent Tag operation.
func pushChart(ctx context.Context, data []byte, destination domain.Endpoint, version string, credential *domain.Credential) error {
	chart, err := helmloader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("load chart for push: %w", err)
	}
	if chartName(destination.Repository) != chart.Metadata.Name || ociTag(version) != ociTag(chart.Metadata.Version) {
		return fmt.Errorf("push target name/version must match Chart.yaml")
	}
	config, err := json.Marshal(chart.Metadata)
	if err != nil {
		return fmt.Errorf("marshal Helm config: %w", err)
	}
	store := memory.New()
	configDesc, err := oras.PushBytes(ctx, store, registry.ConfigMediaType, config)
	if err != nil {
		return fmt.Errorf("store Helm config: %w", err)
	}
	layerDesc, err := oras.PushBytes(ctx, store, registry.ChartLayerMediaType, data)
	if err != nil {
		return fmt.Errorf("store chart layer: %w", err)
	}
	annotations := map[string]string{
		ocispec.AnnotationTitle:   chart.Metadata.Name,
		ocispec.AnnotationVersion: chart.Metadata.Version,
	}
	for key, value := range chart.Metadata.Annotations {
		if strings.HasPrefix(key, "org.opencontainers.image.") {
			annotations[key] = value
		}
	}
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      configDesc,
		Layers:      []ocispec.Descriptor{layerDesc},
		Annotations: annotations,
	})
	if err != nil {
		return fmt.Errorf("marshal Helm manifest: %w", err)
	}
	_, err = oras.TagBytes(ctx, store, ocispec.MediaTypeImageManifest, manifest, "chart")
	if err != nil {
		return fmt.Errorf("store Helm manifest: %w", err)
	}

	host := extractHost(destination.Registry)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	repository, err := remote.NewRepository(host + "/" + destination.Repository)
	if err != nil {
		return fmt.Errorf("create Helm destination: %w", err)
	}
	repository.PlainHTTP = needsPlainHTTP(destination.Registry)
	client := http.DefaultClient
	if !repository.PlainHTTP {
		if destination.Insecure {
			client = insecureHTTPClient
		} else if destination.CACertPath != "" {
			client, err = caCertHTTPClient(destination.CACertPath)
			if err != nil {
				return fmt.Errorf("configure Helm destination CA: %w", err)
			}
		}
	}
	authorizer := &auth.Client{Client: client, Cache: auth.NewCache()}
	if credential != nil {
		authorizer.Credential = auth.StaticCredential(repository.Reference.Registry, auth.Credential{
			Username: credential.Username,
			Password: credential.Password,
		})
	}
	repository.Client = authorizer
	// Copy uses PushReference to write the final tag directly. It does not need
	// to fetch an untagged manifest back from the registry to create the tag.
	if _, err := oras.Copy(ctx, store, "chart", repository, ociTag(version), oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("copy Helm OCI chart: %w", err)
	}
	return nil
}
