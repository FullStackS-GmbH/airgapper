package pattern_test

import (
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/pattern"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"literal version", "v1.0.0", false},
		{"literal tag", "latest", false},
		{"star wildcard", "v1.*", true},
		{"plus quantifier", "v1.0+", true},
		{"character class", "v[12].0", true},
		{"curly braces", "v{1,2}", true},
		{"parentheses", "v(1|2)", true},
		{"backslash", `v1\.0`, true},
		{"caret", "^v1", true},
		{"dollar", "v1$", true},
		{"pipe", "v1|v2", true},
		{"question mark", "v1.?", true},
		{"empty string", "", false},
		{"plain SHA", "abc123def456", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, pattern.IsPattern(tt.input))
		})
	}
}

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pattern    string
		candidates []string
		want       []string
		wantErr    bool
	}{
		{
			name:       "exact match via regex",
			pattern:    "v1\\.0\\.0",
			candidates: []string{"v1.0.0", "v1.0.1", "v2.0.0"},
			want:       []string{"v1.0.0"},
		},
		{
			name:       "wildcard match",
			pattern:    "v1\\..*",
			candidates: []string{"v1.0.0", "v1.2.3", "v2.0.0"},
			want:       []string{"v1.0.0", "v1.2.3"},
		},
		{
			name:       "no matches",
			pattern:    "v3\\..*",
			candidates: []string{"v1.0.0", "v2.0.0"},
			want:       nil,
		},
		{
			name:       "empty candidates",
			pattern:    "v1\\..*",
			candidates: []string{},
			want:       nil,
		},
		{
			name:       "invalid regex",
			pattern:    "[invalid",
			candidates: []string{"anything"},
			wantErr:    true,
		},
		{
			name:       "full match semantics - no partial",
			pattern:    "v1",
			candidates: []string{"v1", "v1.0", "xv1x"},
			want:       []string{"v1"},
		},
		{
			name:       "alternation",
			pattern:    "v1\\.0|v2\\.0",
			candidates: []string{"v1.0", "v2.0", "v3.0"},
			want:       []string{"v1.0", "v2.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := pattern.Match(tt.pattern, tt.candidates)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
