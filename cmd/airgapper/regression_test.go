package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegressionCLIExitCodes(t *testing.T) {
	t.Parallel()
	invalidConfig := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(invalidConfig, []byte("resources:\n  - type: unsupported\n"), 0o600))
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"success", []string{"version"}, 0},
		{"unknown flag", []string{"sync", "--unknown"}, 2},
		{"missing config", []string{"sync", "--config="}, 2},
		{"invalid config", []string{"sync", "--config", invalidConfig}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Reuse the test executable to exercise main's actual os.Exit path.
			args := append([]string{"-test.run=^TestRegressionCLIProcess$", "--"}, tc.args...)
			cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "AIRGAPPER_") && !strings.HasPrefix(env, "AIRGAPPER_TEST_MAIN=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, "AIRGAPPER_TEST_MAIN=1")
			output, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				require.True(t, errors.As(err, &exitErr), "start CLI helper: %v", err)
				code = exitErr.ExitCode()
			}
			assert.Equal(t, tc.code, code, "CLI output: %s", output)
		})
	}
}

func TestRegressionCLIProcess(t *testing.T) {
	if os.Getenv("AIRGAPPER_TEST_MAIN") != "1" {
		return
	}
	os.Args = append([]string{"airgapper"}, os.Args[3:]...)
	main()
	os.Exit(0)
}
