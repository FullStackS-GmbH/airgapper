// Package cli defines the Cobra command tree for the Universal Airgapper CLI.
// It wires together configuration loading, credential resolution, transport
// creation, and the sync engine.
package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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

	// Persistent flags available to all subcommands.
	pflags := cmd.PersistentFlags()
	pflags.StringP("config", "c", "", "Path to config file or folder")
	pflags.String("credentials", "", "Path to credentials file or folder")
	pflags.BoolP("debug", "d", false, "Enable debug logging")
	pflags.Bool("dry-run", false, "Disable all write/push operations")
	pflags.String("log-format", "json", "Log format: json or text")
	pflags.String("dry-run-log", "", "Path for dry-run log file (default: auto-generated)")

	// Bind flags to viper for environment variable support.
	viper.SetEnvPrefix("AIRGAPPER")
	viper.AutomaticEnv()

	_ = viper.BindPFlag("config", pflags.Lookup("config"))
	_ = viper.BindPFlag("credentials", pflags.Lookup("credentials"))
	_ = viper.BindPFlag("debug", pflags.Lookup("debug"))
	_ = viper.BindPFlag("dry_run", pflags.Lookup("dry-run"))
	_ = viper.BindPFlag("log_format", pflags.Lookup("log-format"))
	_ = viper.BindPFlag("dry_run_log", pflags.Lookup("dry-run-log"))

	// Bind environment variables explicitly.
	_ = viper.BindEnv("config", "AIRGAPPER_CONFIG")
	_ = viper.BindEnv("credentials", "AIRGAPPER_CREDENTIALS")
	_ = viper.BindEnv("debug", "AIRGAPPER_DEBUG")
	_ = viper.BindEnv("dry_run", "AIRGAPPER_DRY_RUN")
	_ = viper.BindEnv("log_format", "AIRGAPPER_LOG_FORMAT")
	_ = viper.BindEnv("dry_run_log", "AIRGAPPER_DRY_RUN_LOG")

	// Register subcommands.
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newVersionCmd(version, commit, date))

	return cmd
}
