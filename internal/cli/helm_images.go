package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/helmimages"
	"github.com/fullstacks-gmbh/airgapper/internal/logging"
)

func newHelmImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Extract container image references from Helm charts",
		RunE:  runHelmImages,
	}
	cmd.Flags().StringP("output", "o", "", "Path to write the generated image config YAML")
	cmd.Flags().String("target-credentials-ref", "", "Name of a helm credential entry (its Name field is used as destination registry)")
	_ = cmd.MarkFlagRequired("output")
	_ = cmd.MarkFlagRequired("target-credentials-ref")
	return cmd
}

func runHelmImages(cmd *cobra.Command, _ []string) error {
	configPath := viper.GetString("config")
	credsPath := viper.GetString("credentials")
	debug := viper.GetBool("debug")
	logFormat := viper.GetString("log_format")
	targetCredRef, _ := cmd.Flags().GetString("target-credentials-ref")
	outputPath, _ := cmd.Flags().GetString("output")

	logger := logging.NewLogger(debug, logFormat)

	if configPath == "" {
		return fmt.Errorf("--config flag or AIRGAPPER_CONFIG env var is required")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var credStore domain.CredentialStore
	if credsPath != "" {
		creds, err := config.LoadCredentials(credsPath)
		if err != nil {
			return fmt.Errorf("load credentials: %w", err)
		}
		credStore = credentials.NewFileStore(creds)
	} else {
		credStore = credentials.NewFileStore(nil)
	}

	// Validate target credentials ref and resolve registry hostname early.
	targetCred, err := credStore.ResolveByRef(targetCredRef)
	if err != nil {
		return fmt.Errorf("resolve target credentials ref %q: %w", targetCredRef, err)
	}

	var helmResources []domain.Resource
	for i := range cfg.Resources {
		res := cfg.Resources[i].ToResource()
		if res.Type == domain.ResourceTypeHelm {
			helmResources = append(helmResources, res)
		}
	}

	if len(helmResources) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No helm resources found in config.")
		return nil
	}

	extractor := helmimages.New(logger)
	entries, err := extractor.Extract(cmd.Context(), helmResources, credStore)
	if err != nil {
		return fmt.Errorf("extract images: %w", err)
	}

	output, err := helmimages.BuildOutputYAML(entries, targetCred.Name, targetCredRef)
	if err != nil {
		return fmt.Errorf("build output YAML: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("write output %q: %w", outputPath, err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d image entries to %s\n", len(entries), outputPath)
	return nil
}
