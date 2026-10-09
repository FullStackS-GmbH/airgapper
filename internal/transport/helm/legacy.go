package helm

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

const (
	maxLegacyIndexSize  = 50 << 20
	maxChartArchiveSize = 512 << 20
)

// legacyHTTPClient bounds every legacy-repo request. http.DefaultClient has no
// timeouts at all, so a half-open connection to a chart repository hangs the
// run forever — --timeout is optional and often unset. The limits are on
// connection setup and time-to-first-byte rather than total duration, so a
// large chart on a slow air-gap link still downloads.
var legacyHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

// insecureLegacyHTTPClient mirrors legacyHTTPClient but skips TLS certificate
// verification, for endpoints explicitly marked insecure.
var insecureLegacyHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit opt-in via endpoint.Insecure
	},
}

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

	data, err := httpGet(ctx, chartURL, creds, maxChartArchiveSize, endpoint.Insecure, endpoint.CACertPath)
	if err != nil {
		return nil, "", fmt.Errorf("download chart %q: %w", chartURL, err)
	}

	return data, chartURL, nil
}

func (t *Transporter) legacyChartVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]legacyChartVersion, error) {
	idx, err := fetchLegacyIndex(ctx, endpoint.Registry, creds, endpoint.Insecure, endpoint.CACertPath)
	if err != nil {
		return nil, err
	}

	entries, ok := idx.Entries[endpoint.Repository]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("chart %q: %w", endpoint.Repository, domain.ErrNotFound)
	}
	return entries, nil
}

func fetchLegacyIndex(ctx context.Context, registry string, creds *domain.Credential, insecure bool, certPath string) (*legacyIndex, error) {
	indexURL := legacyRepoBaseURL(registry) + "/index.yaml"
	data, err := httpGet(ctx, indexURL, creds, maxLegacyIndexSize, insecure, certPath)
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

// legacyClientFor selects the HTTP client to use for a legacy chart repo
// request. Insecure takes precedence over certPath; when neither is set, the
// shared timeout-bounded default client is reused.
func legacyClientFor(insecure bool, certPath string) (*http.Client, error) {
	switch {
	case insecure:
		return insecureLegacyHTTPClient, nil
	case certPath != "":
		pool, err := caCertPool(certPath)
		if err != nil {
			return nil, err
		}
		return &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				TLSClientConfig:       &tls.Config{RootCAs: pool},
			},
		}, nil
	default:
		return legacyHTTPClient, nil
	}
}

func httpGet(ctx context.Context, rawURL string, creds *domain.Credential, limit int64, insecure bool, certPath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if creds != nil && (creds.Username != "" || creds.Password != "") {
		req.SetBasicAuth(creds.Username, creds.Password)
	}

	client, err := legacyClientFor(insecure, certPath)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

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
