package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCmd builds a command carrying the same flag shapes the real root uses.
func newTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "test", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().String("log-format", "json", "")
	cmd.Flags().Bool("debug", false, "")
	cmd.Flags().Int("timeout", 0, "")

	cmd.SetArgs(args)
	cmd.SetOut(nil)
	require.NoError(t, cmd.Execute())

	return cmd
}

func TestStringFlag_Precedence(t *testing.T) {
	t.Run("flag default when nothing is set", func(t *testing.T) {
		cmd := newTestCmd(t)
		assert.Equal(t, "json", stringFlag(cmd, "log-format", "LOG_FORMAT"))
	})

	t.Run("environment overrides the default", func(t *testing.T) {
		t.Setenv("AIRGAPPER_LOG_FORMAT", "text")
		cmd := newTestCmd(t)
		assert.Equal(t, "text", stringFlag(cmd, "log-format", "LOG_FORMAT"))
	})

	t.Run("explicit flag beats the environment", func(t *testing.T) {
		t.Setenv("AIRGAPPER_LOG_FORMAT", "text")
		cmd := newTestCmd(t, "--log-format", "json")
		assert.Equal(t, "json", stringFlag(cmd, "log-format", "LOG_FORMAT"))
	})

	t.Run("unknown flag yields empty", func(t *testing.T) {
		cmd := newTestCmd(t)
		assert.Empty(t, stringFlag(cmd, "nonexistent", "NONEXISTENT"))
	})
}

func TestBoolFlag(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cmd := newTestCmd(t)
		v, err := boolFlag(cmd, "debug", "DEBUG")
		require.NoError(t, err)
		assert.False(t, v)
	})

	t.Run("from environment", func(t *testing.T) {
		t.Setenv("AIRGAPPER_DEBUG", "true")
		cmd := newTestCmd(t)
		v, err := boolFlag(cmd, "debug", "DEBUG")
		require.NoError(t, err)
		assert.True(t, v)
	})

	t.Run("explicit flag beats the environment", func(t *testing.T) {
		t.Setenv("AIRGAPPER_DEBUG", "true")
		cmd := newTestCmd(t, "--debug=false")
		v, err := boolFlag(cmd, "debug", "DEBUG")
		require.NoError(t, err)
		assert.False(t, v)
	})

	t.Run("garbage in the environment is an error, not a silent false", func(t *testing.T) {
		t.Setenv("AIRGAPPER_DEBUG", "yes-please")
		cmd := newTestCmd(t)
		_, err := boolFlag(cmd, "debug", "DEBUG")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AIRGAPPER_DEBUG")
	})
}

func TestIntFlag(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cmd := newTestCmd(t)
		v, err := intFlag(cmd, "timeout", "TIMEOUT")
		require.NoError(t, err)
		assert.Equal(t, 0, v)
	})

	t.Run("from environment", func(t *testing.T) {
		t.Setenv("AIRGAPPER_TIMEOUT", "90")
		cmd := newTestCmd(t)
		v, err := intFlag(cmd, "timeout", "TIMEOUT")
		require.NoError(t, err)
		assert.Equal(t, 90, v)
	})

	t.Run("garbage in the environment is an error, not a silent zero", func(t *testing.T) {
		// A silent zero would disable the run timeout entirely.
		t.Setenv("AIRGAPPER_TIMEOUT", "5m")
		cmd := newTestCmd(t)
		_, err := intFlag(cmd, "timeout", "TIMEOUT")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AIRGAPPER_TIMEOUT")
	})
}

func TestWithRunTimeout(t *testing.T) {
	t.Run("zero means no deadline", func(t *testing.T) {
		cmd := newTestCmd(t)
		ctx, cancel, err := withRunTimeout(cmd)
		require.NoError(t, err)
		defer cancel()

		_, ok := ctx.Deadline()
		assert.False(t, ok)
	})

	t.Run("positive value sets a deadline", func(t *testing.T) {
		cmd := newTestCmd(t, "--timeout", "30")
		ctx, cancel, err := withRunTimeout(cmd)
		require.NoError(t, err)
		defer cancel()

		_, ok := ctx.Deadline()
		assert.True(t, ok)
	})

	t.Run("unparseable environment value fails the run", func(t *testing.T) {
		t.Setenv("AIRGAPPER_TIMEOUT", "forever")
		cmd := newTestCmd(t)
		_, _, err := withRunTimeout(cmd)
		require.Error(t, err)
	})
}
