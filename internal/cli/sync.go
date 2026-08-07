package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/logging"
	"github.com/fullstacks-gmbh/airgapper/internal/scanner"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
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
	// Read settings from flags, falling back to AIRGAPPER_* env vars.
	configPath := stringFlag(cmd, "config", "CONFIG")
	credsPath := stringFlag(cmd, "credentials", "CREDENTIALS")
	logFormat := stringFlag(cmd, "log-format", "LOG_FORMAT")
	dryRunLogPath := stringFlag(cmd, "dry-run-log", "DRY_RUN_LOG")

	debug, err := boolFlag(cmd, "debug", "DEBUG")
	if err != nil {
		return err
	}
	dryRun, err := boolFlag(cmd, "dry-run", "DRY_RUN")
	if err != nil {
		return err
	}

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

	// Create the sync engine with a transporter per supported resource type.
	transporters := []domain.Transporter{image.New(logger), helm.New(logger), git.New(logger)}
	engine := sync.NewEngine(transporters, scanner.NewFromConfig(cfg.Scanners), logger)

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

	// Run the sync engine, bounded by the optional --timeout.
	ctx, cancel, err := withRunTimeout(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	results, runErr := engine.Run(ctx, resources, opts)

	// Print whatever the run produced before acting on runErr. A run cut short
	// by a timeout has still mirrored real artifacts, and the operator needs
	// the report of what landed and what did not.
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

	if runErr != nil {
		return fmt.Errorf("sync engine: %w", runErr)
	}

	if hasFailures {
		return fmt.Errorf("sync completed with %d failures", summary.Failed)
	}

	return nil
}
