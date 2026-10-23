// Package scanner implements the domain.Scanner interface by executing external
// commands. It substitutes artifact metadata into a command template and
// interprets the process exit code to determine pass/fail.
package scanner

import (
	"bytes"
	"context"
	"errors"
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
	// Split the template into arguments first, honoring quotes so a configured
	// command like `trivy image "{source}"` survives spaces in a quoted arg.
	// Placeholders are substituted afterwards, inside each argument: values such
	// as a chart version from a remote index.yaml are untrusted, and quotes or
	// spaces in them must not be able to add arguments.
	args := splitArgs(cs.command)
	if len(args) == 0 {
		return nil, fmt.Errorf("scanner %q: empty command", cs.name)
	}
	replacer := cs.replacer(artifact)
	for i, a := range args {
		args[i] = replacer.Replace(a)
	}

	// Create a timeout-bounded context for the command execution.
	cmdCtx, cancel := context.WithTimeout(ctx, cs.timeout)
	defer cancel()

	// Build and execute the command.
	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)

	stdout, stderr := &cappedBuffer{max: maxCapturedOutput}, &cappedBuffer{max: maxCapturedOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	// Determine the exit code.
	exitCode := 0
	if err != nil {
		// Check if this is an ExitError (command ran but returned non-zero).
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
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

// maxCapturedOutput caps how much of each output stream is kept in memory.
const maxCapturedOutput = 64 << 10

// cappedBuffer keeps the first max bytes written and discards the rest, so a
// chatty scanner cannot grow memory without bound. Write always reports the
// full length so the command is never blocked or failed by the cap.
//
// The buffer is a named field, not embedded: embedding would promote
// bytes.Buffer.ReadFrom, which io.Copy prefers over Write and so skips the cap.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// replacer maps placeholders to values from the artifact reference.
func (cs *CommandScanner) replacer(artifact domain.ArtifactRef) *strings.Replacer {
	return strings.NewReplacer(
		"{registry}", artifact.Registry,
		"{repository}", artifact.Repository,
		"{tag}", artifact.Version,
		"{source}", artifact.FullRef(),
		"{type}", artifact.Type.String(),
	)
}

// splitArgs splits a command string into arguments, honoring single and double
// quotes and backslash escapes so that a quoted argument containing spaces
// survives as one token. It is a small shell-like tokenizer, not a full shell:
// it performs no variable/glob expansion.
func splitArgs(s string) []string {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			inArg = true
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
			inArg = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}
