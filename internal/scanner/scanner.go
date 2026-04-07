// Package scanner implements the domain.Scanner interface by executing external
// commands. It substitutes artifact metadata into a command template and
// interprets the process exit code to determine pass/fail.
package scanner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fullstacks-gmbh/airgapper/internal/config"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// defaultTimeout is the fallback command timeout when none is configured.
const defaultTimeout = 300 * time.Second

// CommandScanner executes an external command to scan artifacts. It implements
// the domain.Scanner interface. The command string may contain placeholders
// that are substituted with artifact metadata before execution.
type CommandScanner struct {
	// name is the scanner identifier, matching the scanner_ref in config.
	name string

	// command is the command template with placeholders for substitution.
	command string

	// successCode is the expected process exit code indicating a passing scan.
	successCode int

	// timeout is the maximum duration the scanner command is allowed to run.
	timeout time.Duration
}

// Ensure CommandScanner implements domain.Scanner at compile time.
var _ domain.Scanner = (*CommandScanner)(nil)

// New creates a CommandScanner with the given name, command template, expected
// success exit code, and timeout duration.
func New(name, command string, successCode int, timeout time.Duration) *CommandScanner {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &CommandScanner{
		name:        name,
		command:     command,
		successCode: successCode,
		timeout:     timeout,
	}
}

// NewFromConfig creates a map of scanner name to domain.Scanner from a slice
// of config.ScannerConfig definitions. Each scanner is keyed by its Name field
// for fast lookup when processing resources.
func NewFromConfig(configs []config.ScannerConfig) map[string]domain.Scanner {
	scanners := make(map[string]domain.Scanner, len(configs))
	for _, sc := range configs {
		timeout := time.Duration(sc.Timeout) * time.Second
		scanners[sc.Name] = New(sc.Name, sc.Command, sc.SuccessCode, timeout)
	}
	return scanners
}

// Name returns the scanner identifier.
func (cs *CommandScanner) Name() string {
	return cs.name
}

// Scan executes the configured command against the given artifact reference.
// It substitutes placeholders in the command template, runs the command with
// a timeout context, and returns a ScanResult based on the process exit code.
//
// If the command binary cannot be found or another execution error occurs (not
// an exit code mismatch), Scan returns a Go error. If the command runs but
// exits with an unexpected code, Scan returns a ScanResult with Passed=false
// and a nil error.
func (cs *CommandScanner) Scan(ctx context.Context, artifact domain.ArtifactRef) (*domain.ScanResult, error) {
	// Substitute placeholders in the command template.
	expanded := cs.substituteCommand(artifact)

	// Split the command into arguments using simple whitespace splitting.
	args := strings.Fields(expanded)
	if len(args) == 0 {
		return nil, fmt.Errorf("scanner %q: empty command after expansion", cs.name)
	}

	// Create a timeout-bounded context for the command execution.
	cmdCtx, cancel := context.WithTimeout(ctx, cs.timeout)
	defer cancel()

	// Build and execute the command.
	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Determine the exit code.
	exitCode := 0
	if err != nil {
		// Check if this is an ExitError (command ran but returned non-zero).
		var exitErr *exec.ExitError
		if ok := isExitError(err, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// The command could not be executed at all (binary not found, etc.).
			return nil, fmt.Errorf("scanner %q: execute command: %w", cs.name, err)
		}
	}

	result := &domain.ScanResult{
		Passed:      exitCode == cs.successCode,
		Output:      stdout.String(),
		ErrorOutput: stderr.String(),
		ExitCode:    exitCode,
	}

	return result, nil
}

// substituteCommand replaces placeholders in the command template with values
// from the artifact reference.
func (cs *CommandScanner) substituteCommand(artifact domain.ArtifactRef) string {
	r := strings.NewReplacer(
		"{registry}", artifact.Registry,
		"{repository}", artifact.Repository,
		"{tag}", artifact.Version,
		"{source}", artifact.FullRef(),
		"{type}", artifact.Type.String(),
	)
	return r.Replace(cs.command)
}

// isExitError checks whether err is an *exec.ExitError and, if so, assigns it
// to the target pointer. This is a helper to keep the Scan method readable.
func isExitError(err error, target **exec.ExitError) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		*target = exitErr
		return true
	}
	return false
}
