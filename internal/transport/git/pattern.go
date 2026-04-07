package git

import "github.com/fullstacks-gmbh/airgapper/internal/pattern"

// IsPattern returns true if the string contains regex metacharacters.
// It delegates to the shared pattern package.
func IsPattern(s string) bool {
	return pattern.IsPattern(s)
}

// MatchRefs filters the given refs against a regex pattern using full-match
// semantics. It delegates to the shared pattern package.
func MatchRefs(p string, refs []string) ([]string, error) {
	return pattern.Match(p, refs)
}
