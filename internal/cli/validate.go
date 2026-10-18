package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// newValidateCmd creates the "validate" subcommand, which checks the
// configuration and credentials without contacting any registry or host.
func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check configuration and credentials without syncing",
		RunE:  runValidate,
	}
	cmd.Flags().String("output", "text", "Result format: text or json (env: AIRGAPPER_OUTPUT)")
	return cmd
}

// validateReport is the JSON form of a validation run.
type validateReport struct {
	Valid     bool     `json:"valid"`
	Resources int      `json:"resources"`
	Scanners  int      `json:"scanners"`
	Problems  []string `json:"problems"`
}

func runValidate(cmd *cobra.Command, _ []string) error {
	configPath := stringFlag(cmd, "config", "CONFIG")
	credsPath := stringFlag(cmd, "credentials", "CREDENTIALS")
	if configPath == "" {
		return usage(errors.New("--config flag or AIRGAPPER_CONFIG env var is required"))
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	// A file that cannot be read or parsed stops the check: there is nothing
	// further to inspect.
	cfg, err := config.Load(configPath)
	if err != nil {
		return usage(fmt.Errorf("load config: %w", err))
	}
	var creds []domain.Credential
	if credsPath != "" {
		if creds, err = config.LoadCredentials(credsPath); err != nil {
			return usage(fmt.Errorf("load credentials: %w", err))
		}
	}

	var problems []string
	for _, err := range []error{config.Validate(cfg), config.Check(cfg, creds, credsPath != "")} {
		problems = append(problems, splitJoined(err)...)
	}

	report := validateReport{
		Valid:     len(problems) == 0,
		Resources: len(cfg.Resources),
		Scanners:  len(cfg.Scanners),
		Problems:  append([]string{}, problems...),
	}

	out := cmd.OutOrStdout()
	if format == "json" {
		if err := json.NewEncoder(out).Encode(report); err != nil {
			return fmt.Errorf("write json report: %w", err)
		}
	} else if report.Valid {
		_, _ = fmt.Fprintf(out, "Configuration OK: %d resources, %d scanners\n", report.Resources, report.Scanners)
	} else {
		for _, p := range problems {
			_, _ = fmt.Fprintf(out, "- %s\n", p)
		}
	}

	if !report.Valid {
		return usage(fmt.Errorf("configuration has %d problem(s)", len(problems)))
	}
	return nil
}

// splitJoined returns the messages of an error built with errors.Join, one per
// joined error, or the single message of any other error. Nil gives none.
func splitJoined(err error) []string {
	if err == nil {
		return nil
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		var out []string
		for _, e := range joined.Unwrap() {
			out = append(out, splitJoined(e)...)
		}
		return out
	}
	return []string{err.Error()}
}
