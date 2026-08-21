// Package cli defines the Cobra command tree for the Universal Airgapper CLI.
// It wires together configuration loading, credential resolution, transport
// creation, and the sync engine.
package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// envPrefix is prepended to the name of every environment variable the CLI
// reads.
const envPrefix = "AIRGAPPER_"

// rawFlag resolves a single setting to its string form. An explicitly provided
// command-line flag wins; otherwise the environment variable is used; otherwise
// the flag's default. The second return value reports whether the value came
// from the environment, so callers parsing non-string types can name the source
// in their error message.
func rawFlag(cmd *cobra.Command, name, env string) (string, bool) {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return "", false
	}
	if !f.Changed {
		if v, ok := os.LookupEnv(envPrefix + env); ok {
			return v, true
		}
	}
	return f.Value.String(), false
}

// stringFlag resolves a string setting from flag or environment.
func stringFlag(cmd *cobra.Command, name, env string) string {
	v, _ := rawFlag(cmd, name, env)
	return v
}

// boolFlag resolves a boolean setting from flag or environment. An
// unparseable environment value is an error rather than a silent false.
func boolFlag(cmd *cobra.Command, name, env string) (bool, error) {
	v, fromEnv := rawFlag(cmd, name, env)
	b, err := strconv.ParseBool(v)
	if err != nil {
		if fromEnv {
			return false, fmt.Errorf("%s%s=%q is not a boolean", envPrefix, env, v)
		}
		return false, fmt.Errorf("--%s=%q is not a boolean", name, v)
	}
	return b, nil
}

// intFlag resolves an integer setting from flag or environment. An unparseable
// environment value is an error rather than a silent zero.
func intFlag(cmd *cobra.Command, name, env string) (int, error) {
	v, fromEnv := rawFlag(cmd, name, env)
	n, err := strconv.Atoi(v)
	if err != nil {
		if fromEnv {
			return 0, fmt.Errorf("%s%s=%q is not an integer", envPrefix, env, v)
		}
		return 0, fmt.Errorf("--%s=%q is not an integer", name, v)
	}
	return n, nil
}

// withRunTimeout derives a context bounded by the --timeout flag (seconds).
// A value of 0 disables the timeout and returns a no-op cancel. Callers must
// defer the returned cancel.
func withRunTimeout(cmd *cobra.Command) (context.Context, context.CancelFunc, error) {
	sec, err := intFlag(cmd, "timeout", "TIMEOUT")
	if err != nil {
		return nil, nil, err
	}
	if sec > 0 {
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(sec)*time.Second)
		return ctx, cancel, nil
	}
	return cmd.Context(), func() {}, nil
}

// NewRootCmd creates the root command with all subcommands registered. The
// version, commit, and date parameters are typically injected via ldflags at
// build time.
func NewRootCmd(version, commit, date string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "airgapper",
		Short:         "Universal Airgapper -- sync artifacts across registries",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags available to all subcommands. Each has a matching
	// AIRGAPPER_* environment variable; see rawFlag for the precedence rules.
	pflags := cmd.PersistentFlags()
	pflags.StringP("config", "c", "", "Path to config file or folder (env: AIRGAPPER_CONFIG)")
	pflags.String("credentials", "", "Path to credentials file or folder (env: AIRGAPPER_CREDENTIALS)")
	pflags.BoolP("debug", "d", false, "Enable debug logging (env: AIRGAPPER_DEBUG)")
	pflags.Bool("dry-run", false, "Disable all write/push operations (env: AIRGAPPER_DRY_RUN)")
	pflags.String("log-format", "json", "Log format: json or text (env: AIRGAPPER_LOG_FORMAT)")
	pflags.String("dry-run-log", "", "Path for dry-run log file, default auto-generated (env: AIRGAPPER_DRY_RUN_LOG)")
	pflags.Int("timeout", 0, "Overall run timeout in seconds, 0 = no timeout (env: AIRGAPPER_TIMEOUT)")

	// Register subcommands.
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newVersionCmd(version, commit, date))
	cmd.AddCommand(newHelmCmd())

	return cmd
}
