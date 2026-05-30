// Package helmimages extracts container image references from rendered Helm charts.
package helmimages

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	helmtransport "github.com/fullstacks-gmbh/airgapper/internal/transport/helm"
)

// ImageEntry represents a unique container image source with all tags
// found across rendered Helm charts.
type ImageEntry struct {
	Registry   string
	Repository string
	Tags       []TagEntry
}

// TagEntry holds a single image tag and the Helm chart references that use it.
type TagEntry struct {
	Tag     string
	Sources []string // e.g. ["bitnamicharts/nginx:18.3.5"]
}

// Extractor pulls Helm charts, renders them, and extracts container image references.
type Extractor struct {
	transporter *helmtransport.Transporter
	logger      *slog.Logger
}

// New creates a new Extractor.
func New(logger *slog.Logger) *Extractor {
	return &Extractor{
		transporter: helmtransport.New(logger),
		logger:      logger,
	}
}

// Extract processes all Helm resources, renders each chart version with default
// values, and returns a deduplicated list of image entries in insertion order.
// Render failures are logged as warnings and that version is skipped.
func (e *Extractor) Extract(ctx context.Context, resources []domain.Resource, credStore domain.CredentialStore) ([]ImageEntry, error) {
	var imageOrder []string
	tagOrder := map[string][]string{}
	sources := map[string]map[string][]string{}

	addImage := func(registry, repository, tag, source string) {
		key := registry + "/" + repository
		if _, ok := sources[key]; !ok {
			imageOrder = append(imageOrder, key)
			sources[key] = map[string][]string{}
		}
		if _, ok := sources[key][tag]; !ok {
			tagOrder[key] = append(tagOrder[key], tag)
		}
		for _, s := range sources[key][tag] {
			if s == source {
				return
			}
		}
		sources[key][tag] = append(sources[key][tag], source)
	}

	for _, res := range resources {
		if res.Type != domain.ResourceTypeHelm {
			continue
		}
		chartRef := res.Source.Repository

		for _, version := range res.Versions {
			data, err := e.transporter.PullChartBytes(ctx, res, version, credStore)
			if err != nil {
				e.logger.Warn("skipping chart version: pull failed", "chart", chartRef, "version", version, "error", err)
				continue
			}

			ch, err := helmloader.LoadArchive(bytes.NewReader(data))
			if err != nil {
				e.logger.Warn("skipping chart version: load failed", "chart", chartRef, "version", version, "error", err)
				continue
			}

			vals, err := chartutil.ToRenderValues(ch, ch.Values, common.ReleaseOptions{
				Name:      ch.Name(),
				Namespace: "default",
			}, nil)
			if err != nil {
				e.logger.Warn("skipping chart version: render values failed", "chart", chartRef, "version", version, "error", err)
				continue
			}

			rendered, err := engine.Render(ch, vals)
			if err != nil {
				e.logger.Warn("skipping chart version: render failed", "chart", chartRef, "version", version, "error", err)
				continue
			}

			label := fmt.Sprintf("%s:%s", chartRef, version)
			for _, content := range rendered {
				for _, ref := range ExtractImagesFromYAML(content) {
					reg, repo, tag := ParseImageRef(ref)
					if tag == "" || reg == "" {
						continue
					}
					addImage(reg, repo, tag, label)
				}
			}
		}
	}

	entries := make([]ImageEntry, 0, len(imageOrder))
	for _, key := range imageOrder {
		parts := strings.SplitN(key, "/", 2)
		reg, repo := parts[0], parts[1]
		tags := make([]TagEntry, 0, len(tagOrder[key]))
		for _, tag := range tagOrder[key] {
			tags = append(tags, TagEntry{Tag: tag, Sources: sources[key][tag]})
		}
		entries = append(entries, ImageEntry{Registry: reg, Repository: repo, Tags: tags})
	}
	return entries, nil
}

// ParseImageRef parses an image reference string into registry, repository, and
// tag. Docker Hub short names are expanded: "nginx" → docker.io, library/nginx,
// latest. Returns empty tag for digest references.
func ParseImageRef(ref string) (registry, repository, tag string) {
	parsed, err := name.ParseReference(strings.TrimSpace(ref))
	if err != nil {
		return "", ref, ""
	}
	registry = normalizeDockerRegistry(parsed.Context().RegistryStr())
	repository = parsed.Context().RepositoryStr()
	tagged, ok := parsed.(name.Tag)
	if !ok {
		return registry, repository, ""
	}
	tag = tagged.TagStr()
	return registry, repository, tag
}

// normalizeDockerRegistry maps go-containerregistry's internal Docker Hub
// hostname back to the canonical "docker.io".
func normalizeDockerRegistry(reg string) string {
	if reg == "index.docker.io" {
		return "docker.io"
	}
	return reg
}

// ExtractImagesFromYAML walks all YAML documents in content and returns the
// values of every "image:" scalar key found.
func ExtractImagesFromYAML(content string) []string {
	var images []string
	dec := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if doc.Kind == 0 {
			continue
		}
		walkYAMLNode(&doc, &images)
	}
	return images
}

func walkYAMLNode(node *yaml.Node, images *[]string) {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			val := node.Content[i+1]
			if key.Value == "image" && val.Kind == yaml.ScalarNode && val.Value != "" {
				*images = append(*images, val.Value)
			}
			walkYAMLNode(val, images)
		}
		return
	}
	for _, child := range node.Content {
		walkYAMLNode(child, images)
	}
}

// BuildOutputYAML generates a valid airgapper image config YAML from the
// extracted image entries. Tags carry inline comments listing their source
// Helm chart version(s). targetRegistry is the credential's Name field
// (registry hostname); targetCredRef is the credential reference name.
func BuildOutputYAML(entries []ImageEntry, targetRegistry, targetCredRef string) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	doc.Content = []*yaml.Node{root}

	resourcesKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "resources"}
	resourcesSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, resourcesKey, resourcesSeq)

	for _, entry := range entries {
		source := entry.Registry + "/" + entry.Repository
		destination := targetRegistry + "/" + entry.Repository

		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendStrPair(m, "type", "image")
		appendStrPair(m, "source", source)
		appendStrPair(m, "destination", destination)
		appendStrPair(m, "target_credentials_ref", targetCredRef)

		tagsKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "tags"}
		tagsSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, te := range entry.Tags {
			tagNode := &yaml.Node{
				Kind:        yaml.ScalarNode,
				Value:       te.Tag,
				Tag:         "!!str",
				Style:       yaml.DoubleQuotedStyle,
				LineComment: "from helm: " + strings.Join(te.Sources, ", "),
			}
			tagsSeq.Content = append(tagsSeq.Content, tagNode)
		}
		m.Content = append(m.Content, tagsKey, tagsSeq)
		resourcesSeq.Content = append(resourcesSeq.Content, m)
	}

	return yaml.Marshal(doc)
}

func appendStrPair(m *yaml.Node, key, value string) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}
