package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates the "version" subcommand that prints build information
// including the version tag, commit hash, and build date.
func newVersionCmd(version, commit, date string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "airgapper version %s\n", version)
			fmt.Fprintf(cmd.OutOrStdout(), "  commit: %s\n", commit)
			fmt.Fprintf(cmd.OutOrStdout(), "  built:  %s\n", date)
		},
	}
	return cmd
}
