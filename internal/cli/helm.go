package cli

import "github.com/spf13/cobra"

// newHelmCmd creates the "helm" parent command with all helm subcommands registered.
func newHelmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helm",
		Short: "Helm chart operations",
	}
	cmd.AddCommand(newHelmImagesCmd())
	return cmd
}
