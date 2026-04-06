package git_test

import (
	"testing"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/transport/git"
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
		{"literal branch", "main", false},
		{"literal tag", "v1.0.0", false},
		{"regex pattern", "v[0-9]+\\..*", true},
		{"star pattern", "release-*", true},
		{"plain SHA", "abc123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, git.IsPattern(tt.input))
		})
	}
}

func TestMatchRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		refs    []string
		want    []string
		wantErr bool
	}{
		{
			name:    "match release branches",
			pattern: "release-.*",
			refs:    []string{"main", "release-1.0", "release-2.0", "develop"},
			want:    []string{"release-1.0", "release-2.0"},
		},
		{
			name:    "match version tags",
			pattern: "v[0-9]+\\.[0-9]+\\.[0-9]+",
			refs:    []string{"v1.0.0", "v2.1.3", "latest", "main"},
			want:    []string{"v1.0.0", "v2.1.3"},
		},
		{
			name:    "no matches",
			pattern: "hotfix-.*",
			refs:    []string{"main", "develop"},
			want:    nil,
		},
		{
			name:    "invalid regex",
			pattern: "[bad",
			refs:    []string{"main"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := git.MatchRefs(tt.pattern, tt.refs)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
