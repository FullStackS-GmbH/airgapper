package helm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

const (
	maxLegacyIndexSize  = 50 << 20
	maxChartArchiveSize = 512 << 20
)

type legacyIndex struct {
	Entries map[string][]legacyChartVersion `yaml:"entries"`
}

type legacyChartVersion struct {
	Version string   `yaml:"version"`
	URLs    []string `yaml:"urls"`
}

func (t *Transporter) listLegacyVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	entries, err := t.legacyChartVersions(ctx, endpoint, creds)
	if err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Version != "" {
			versions = append(versions, entry.Version)
		}
	}
	return versions, nil
}

func (t *Transporter) legacyChartExists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	entries, err := t.legacyChartVersions(ctx, endpoint, creds)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if entry.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (t *Transporter) pullLegacyChart(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) ([]byte, string, error) {
	entries, err := t.legacyChartVersions(ctx, endpoint, creds)
	if err != nil {
		return nil, "", err
	}

	var chartURL string
	for _, entry := range entries {
		if entry.Version != version {
			continue
		}
		if len(entry.URLs) == 0 {
			return nil, "", fmt.Errorf("chart %q version %q has no download URLs: %w", endpoint.Repository, version, domain.ErrNotFound)
		}
		chartURL, err = resolveLegacyChartURL(legacyRepoBaseURL(endpoint.Registry), entry.URLs[0])
		if err != nil {
			return nil, "", fmt.Errorf("resolve chart URL for %q version %q: %w", endpoint.Repository, version, err)
		}
		break
	}

	if chartURL == "" {
		return nil, "", fmt.Errorf("chart %q version %q: %w", endpoint.Repository, version, domain.ErrNotFound)
	}

	data, err := httpGet(ctx, chartURL, creds, maxChartArchiveSize)
	if err != nil {
		return nil, "", fmt.Errorf("download chart %q: %w", chartURL, err)
	}

	return data, chartURL, nil
}

func (t *Transporter) legacyChartVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]legacyChartVersion, error) {
	idx, err := fetchLegacyIndex(ctx, endpoint.Registry, creds)
	if err != nil {
		return nil, err
	}

	entries, ok := idx.Entries[endpoint.Repository]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("chart %q: %w", endpoint.Repository, domain.ErrNotFound)
	}
	return entries, nil
}

func fetchLegacyIndex(ctx context.Context, registry string, creds *domain.Credential) (*legacyIndex, error) {
	indexURL := legacyRepoBaseURL(registry) + "/index.yaml"
	data, err := httpGet(ctx, indexURL, creds, maxLegacyIndexSize)
	if err != nil {
		return nil, fmt.Errorf("fetch index %q: %w", indexURL, err)
	}

	var idx legacyIndex
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index %q: %w", indexURL, err)
	}
	if idx.Entries == nil {
		idx.Entries = map[string][]legacyChartVersion{}
	}
	return &idx, nil
}

func legacyRepoBaseURL(registry string) string {
	base := strings.TrimSpace(registry)
	base = strings.TrimRight(base, "/")
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return base
}

func resolveLegacyChartURL(base, chartURL string) (string, error) {
	parsedChartURL, err := url.Parse(chartURL)
	if err != nil {
		return "", err
	}
	if parsedChartURL.IsAbs() {
		return parsedChartURL.String(), nil
	}

	parsedBase, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return "", err
	}
	return parsedBase.ResolveReference(parsedChartURL).String(), nil
}

func httpGet(ctx context.Context, rawURL string, creds *domain.Credential, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if creds != nil && (creds.Username != "" || creds.Password != "") {
		req.SetBasicAuth(creds.Username, creds.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%s: %w", resp.Status, domain.ErrAuthFailed)
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%s: %w", resp.Status, domain.ErrNotFound)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}
