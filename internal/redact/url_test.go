package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, input, want string }{
		{"password", "fetch https://user:secret@host/repo", "fetch https://***@host/repo"},
		{"token", "https://token@host/repo", "https://***@host/repo"},
		{"encoded", "https://user:p%40ss%2Fword@host/repo", "https://***@host/repo"},
		{"multiple", "https://a:b@one -> ssh://c:d@two", "https://***@one -> ssh://***@two"},
		{"no credentials", "https://host/repo?x=1", "https://host/repo?x=1"},
		{"path at sign", "https://host/path@tag", "https://host/path@tag"},
		{"email", "contact user@example.com", "contact user@example.com"},
		{"no match across lines", "https://host\nuser@other", "https://host\nuser@other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, URLCredentials(tc.input))
		})
	}
}
