// Package helm implements the Helm chart transport layer supporting both
// OCI-compliant registries and legacy HTTP chart repositories.
package helm

import (
	"fmt"
	"strings"
)

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

	// Strip any scheme for hostname comparison.
	host := registryURL
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}

	// Remove trailing path components.
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	// Remove port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

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

	host := registryURL
	host = strings.TrimPrefix(host, "oci://")
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	return isLocalRegistryHost(host)
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
