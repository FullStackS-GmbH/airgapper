package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/logging"
	"github.com/fullstacks-gmbh/airgapper/internal/scanner"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
	"github.com/fullstacks-gmbh/airgapper/internal/transport"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/git"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/helm"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/image"
)

// newSyncCmd creates the "sync" subcommand that orchestrates artifact
// synchronization from source to destination registries.
func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize artifacts from source to destination",
		RunE:  runSync,
	}
	return cmd
}

// runSync is the RunE handler for the sync subcommand. It loads configuration,
// sets up the transport pipeline, and runs the sync engine.
func runSync(cmd *cobra.Command, _ []string) error {
	// Read settings from viper (flags + env vars).
	configPath := viper.GetString("config")
	credsPath := viper.GetString("credentials")
	debug := viper.GetBool("debug")
	dryRun := viper.GetBool("dry_run")
	logFormat := viper.GetString("log_format")
	dryRunLogPath := viper.GetString("dry_run_log")

	// Initialize structured logger.
	logger := logging.NewLogger(debug, logFormat)

	// Validate required flags.
	if configPath == "" {
		return fmt.Errorf("--config flag or AIRGAPPER_CONFIG env var is required")
	}

	// Load and validate configuration.
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	// Load credentials (optional).
	var credStore domain.CredentialStore
	if credsPath != "" {
		creds, err := config.LoadCredentials(credsPath)
		if err != nil {
			return fmt.Errorf("load credentials: %w", err)
		}
		credStore = credentials.NewFileStore(creds)
	} else {
		// Create an empty credential store for anonymous access.
		credStore = credentials.NewFileStore(nil)
	}

	// Create transporters for all supported resource types.
	imageT := image.New(logger)
	helmT := helm.New(logger)
	gitT := git.New(logger)

	// Create the transport factory.
	factory := transport.NewFactory(imageT, helmT, gitT)

	// Create scanners from configuration.
	scanners := scanner.NewFromConfig(cfg.Scanners)

	// Create the sync engine.
	engine := sync.NewEngine(factory, scanners, logger)

	// Convert config resources to domain resources.
	resources := make([]domain.Resource, 0, len(cfg.Resources))
	for i := range cfg.Resources {
		resources = append(resources, cfg.Resources[i].ToResource())
	}

	// Build sync options.
	opts := domain.SyncOptions{
		DryRun:      dryRun,
		Credentials: credStore,
		Logger:      logger,
	}

	// Run the sync engine.
	results, err := engine.Run(cmd.Context(), resources, opts)
	if err != nil {
		return fmt.Errorf("sync engine: %w", err)
	}

	// Print individual results.
	hasFailures := false
	for _, result := range results {
		source := result.Resource.Source.String()
		dest := result.Resource.Destination.String()

		for _, vr := range result.Synced {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), sync.FormatResult(result.Resource.Type, source, dest, vr))
		}
		for _, vr := range result.Skipped {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), sync.FormatResult(result.Resource.Type, source, dest, vr))
		}
		for _, vr := range result.Failed {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), sync.FormatResult(result.Resource.Type, source, dest, vr))
		}

		if result.HasFailures() {
			hasFailures = true
		}
	}

	// Print summary.
	summary := sync.Summarize(results)
	_, _ = fmt.Fprint(cmd.OutOrStdout(), sync.FormatSummary(summary))

	// Write dry-run log file.
	if dryRun {
		logPath, logErr := sync.WriteDryRunLog(dryRunLogPath, results, summary)
		if logErr != nil {
			logger.Error("failed to write dry-run log", "error", logErr)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dry-run log written to: %s\n", logPath)
		}
	}

	if hasFailures {
		return fmt.Errorf("sync completed with %d failures", summary.Failed)
	}

	return nil
}
