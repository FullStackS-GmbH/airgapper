// Package pattern provides regex-based version and ref matching used across
// all transport types (images, helm charts, git repos) to expand version
// patterns from configuration into concrete version lists.
package pattern

import (
	"fmt"
	"regexp"
	"strings"
)

// metaChars contains the regex metacharacters that indicate a string is a
// pattern rather than a literal version or ref name.
const metaChars = `*+[]{}()\^$|?`

// IsPattern returns true if the string contains regex metacharacters. The
// detected characters are: * + [ ] { } ( ) \ ^ $ | ?
//
// This function is used to distinguish literal versions/tags/refs from regex
// patterns in the configuration file. Literal values are synced directly;
// patterns are expanded against the available versions at the source.
func IsPattern(s string) bool {
	return strings.ContainsAny(s, metaChars)
}

// Match filters the given candidates against a regex pattern. The pattern is
// anchored with ^ and $ for full-match semantics. It returns only the
// candidates that fully match the compiled pattern.
//
// An error is returned if the pattern is not a valid regular expression.
func Match(pattern string, candidates []string) ([]string, error) {
	anchored := fmt.Sprintf("^%s$", pattern)
	re, err := regexp.Compile(anchored)
	if err != nil {
		return nil, fmt.Errorf("compile pattern %q: %w", pattern, err)
	}

	var matched []string
	for _, c := range candidates {
		if re.MatchString(c) {
			matched = append(matched, c)
		}
	}
	return matched, nil
}
