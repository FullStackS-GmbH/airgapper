package domain

import "context"

// Scanner defines the contract for running security or compliance checks on
// artifacts before they are promoted to the destination. The implementation
// executes an external command and interprets the result based on exit code.
//
// Scanner must not modify the artifact or any registry state.
type Scanner interface {
	// Name returns the scanner identifier (matches the scanner_ref in config).
	Name() string

	// Scan runs the configured check against the given artifact reference.
	// It returns a ScanResult indicating pass/fail and captured output.
	//
	// If the scanner command cannot be executed (binary not found, permission
	// denied), Scan returns a Go error.
	// If the command executes but fails the check, Scan returns a ScanResult
	// with Passed=false and a nil error.
	Scan(ctx context.Context, artifact ArtifactRef) (*ScanResult, error)
}
