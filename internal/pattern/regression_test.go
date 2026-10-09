package pattern_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/pattern"
)

func TestRegressionAlternationMatchesWholeVersion(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{"v1|v2", "(v1|v2)"} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			got, err := pattern.Match(expression, []string{"v1", "v2", "v1-extra", "prefix-v2", "prefix-v1-extra"})
			require.NoError(t, err)
			assert.Equal(t, []string{"v1", "v2"}, got)
		})
	}
}
