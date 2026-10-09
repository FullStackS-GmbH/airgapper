// Package redact masks credentials at human-readable output boundaries.
package redact

import "regexp"

var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

// URLCredentials replaces URL userinfo in arbitrary text, preserving the
// scheme, hostname and path so diagnostics remain useful.
func URLCredentials(text string) string {
	return urlUserinfo.ReplaceAllString(text, "${1}***@")
}
