package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

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
	cmd.Flags().StringP("output", "o", "", "Path to write the generated image config YAML (env: AIRGAPPER_HELM_IMAGES_OUTPUT)")
	cmd.Flags().String("target-credentials-ref", "", "Name of a helm credential entry, its Name field is used as destination registry (env: AIRGAPPER_HELM_IMAGES_TARGET_CREDENTIALS_REF)")

	return cmd
}

func runHelmImages(cmd *cobra.Command, _ []string) error {
	configPath := stringFlag(cmd, "config", "CONFIG")
	credsPath := stringFlag(cmd, "credentials", "CREDENTIALS")
	logFormat := stringFlag(cmd, "log-format", "LOG_FORMAT")
	targetCredRef := stringFlag(cmd, "target-credentials-ref", "HELM_IMAGES_TARGET_CREDENTIALS_REF")
	outputPath := stringFlag(cmd, "output", "HELM_IMAGES_OUTPUT")

	debug, err := boolFlag(cmd, "debug", "DEBUG")
	if err != nil {
		return err
	}

	logger := logging.NewLogger(debug, logFormat)

	if configPath == "" {
		return fmt.Errorf("--config flag or AIRGAPPER_CONFIG env var is required")
	}
	if outputPath == "" {
		return fmt.Errorf("--output flag or AIRGAPPER_HELM_IMAGES_OUTPUT env var is required")
	}
	if targetCredRef == "" {
		return fmt.Errorf("--target-credentials-ref flag or AIRGAPPER_HELM_IMAGES_TARGET_CREDENTIALS_REF env var is required")
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
	targetCred, err := credStore.ResolveByRef(targetCredRef, domain.CredentialTypeHelm)
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

	ctx, cancel, err := withRunTimeout(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	extractor := helmimages.New(logger)
	entries, skipped, err := extractor.Extract(ctx, helmResources, credStore)
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

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Wrote %d image entries to %s\n", len(entries), outputPath)

	// Loudly surface incomplete extraction. A mirror built from a partial image
	// set silently causes ImagePullBackOff in the air-gapped environment, so we
	// list every skipped version and exit non-zero to fail CI.
	if len(skipped) > 0 {
		_, _ = fmt.Fprintf(out, "\nWARNING: %d chart version(s) skipped; their images are NOT in the output:\n", len(skipped))
		for _, s := range skipped {
			_, _ = fmt.Fprintf(out, "  - %s:%s (%s)\n", s.Chart, s.Version, s.Reason)
		}
	}

	if len(entries) == 0 {
		return fmt.Errorf("no images extracted from %d helm resource(s); generated config is empty", len(helmResources))
	}
	if len(skipped) > 0 {
		return fmt.Errorf("%d chart version(s) skipped; extracted image set is incomplete", len(skipped))
	}

	return nil
}
