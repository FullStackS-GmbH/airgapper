// Package helm implements the Helm chart transport layer supporting both
// OCI-compliant registries and legacy HTTP chart repositories.
package helm

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// insecureHTTPClient skips TLS certificate verification for OCI registry
// clients whose endpoint was explicitly marked insecure. Shared across
// requests like legacyHTTPClient in legacy.go.
var insecureHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit opt-in via endpoint.Insecure
	},
}

// caCertHTTPClient builds an *http.Client that trusts the "ca.crt" file
// inside certDir in addition to the system pool, following Docker's
// host-cert-directory convention.
func caCertHTTPClient(certDir string) (*http.Client, error) {
	pool, err := caCertPool(certDir)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}, nil
}

// caCertPool loads the system trust store and appends the "ca.crt" file
// inside certDir to it.
func caCertPool(certDir string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	caPath := filepath.Join(certDir, "ca.crt")
	pemData, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert %q: %w", caPath, err)
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("no valid certificates found in %q", caPath)
	}
	return pool, nil
}

// knownOCIHosts lists registry hostnames that are known to be OCI-compliant.
// These registries support the OCI distribution spec and can store Helm charts
// as OCI artifacts.
var knownOCIHosts = map[string]bool{
	"docker.io":                  true,
	"registry-1.docker.io":       true,
	"ghcr.io":                    true,
	"gcr.io":                     true,
	"azurecr.io":                 true,
	"public.ecr.aws":             true,
	"gallery.ecr.aws":            true,
	"quay.io":                    true,
	"registry.gitlab.com":        true,
	"harbor.io":                  true,
	"cr.yandex":                  true,
	"lscr.io":                    true,
	"registry.k8s.io":            true,
	"pkg.dev":                    true,
	"nvcr.io":                    true,
	"registry.suse.com":          true,
	"registry.opensuse.org":      true,
	"registry.access.redhat.com": true,
}

// IsOCIRegistry returns true if the registry URL indicates an OCI-compliant
// registry. A registry is considered OCI if it uses the "oci://" scheme or if
// its hostname is in the known OCI hosts list. Additionally, any hostname
// containing common OCI-capable suffixes (e.g. ".azurecr.io", ".pkg.dev") is
// treated as OCI.
func IsOCIRegistry(registryURL string) bool {
	if strings.HasPrefix(registryURL, "oci://") {
		return true
	}

	hasHTTPScheme := strings.HasPrefix(registryURL, "http://") || strings.HasPrefix(registryURL, "https://")

	host := hostOf(registryURL)

	// Direct match.
	if knownOCIHosts[host] {
		return true
	}

	if !hasHTTPScheme && isLocalRegistryHost(host) {
		return true
	}

	// Suffix match for cloud provider registries.
	ociSuffixes := []string{
		".azurecr.io",
		".pkg.dev",
		".gcr.io",
		".ecr.aws",
	}
	for _, suffix := range ociSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	return false
}

// hostOf strips the scheme, any path components, and the port from a registry
// string, leaving just the hostname.
func hostOf(registryURL string) string {
	host := registryURL
	if _, after, found := strings.Cut(host, "://"); found {
		host = after
	}
	host, _, _ = strings.Cut(host, "/")
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

func isLocalRegistryHost(host string) bool {
	return host == "localhost" ||
		host == "127.0.0.1" ||
		strings.HasPrefix(host, "127.") ||
		host == "0.0.0.0" ||
		host == "host.docker.internal"
}

func needsPlainHTTP(registryURL string) bool {
	if strings.HasPrefix(registryURL, "http://") {
		return true
	}

	return isLocalRegistryHost(hostOf(registryURL))
}

// NormalizeOCIRef builds an OCI reference string for a Helm chart. The
// returned string has the form "oci://registry/chart:version". If the
// registry already has an "oci://" prefix it is not duplicated.
func NormalizeOCIRef(registry, chart, version string) string {
	base := strings.TrimPrefix(registry, "oci://")
	base = strings.TrimRight(base, "/")

	ref := fmt.Sprintf("oci://%s/%s", base, chart)
	if version != "" {
		ref += ":" + version
	}
	return ref
}
