package domain

import "errors"

// Sentinel errors used across the application. Lower-level packages wrap these
// with additional context using fmt.Errorf("...: %w", err). Callers match on
// them with errors.Is.

var (
	// ErrNotFound indicates that a requested resource, version, or artifact
	// does not exist at the expected location.
	ErrNotFound = errors.New("not found")

	// ErrAuthFailed indicates that authentication against a registry or host
	// failed (HTTP 401/403 or SSH key rejection).
	ErrAuthFailed = errors.New("authentication failed")

	// ErrAlreadyExists indicates that the artifact version already exists at
	// the destination. This is informational and used with PushModeSkip.
	ErrAlreadyExists = errors.New("already exists")

	// ErrScanFailed indicates that a scanner reported a failure for the
	// artifact (the scan ran but the artifact did not pass).
	ErrScanFailed = errors.New("scan failed")

	// ErrInvalidConfig indicates that configuration validation failed during
	// startup.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrTimeout indicates that an operation exceeded its deadline.
	ErrTimeout = errors.New("operation timed out")

	// ErrTransportFailed indicates a transport-level failure such as a network
	// error, protocol mismatch, or registry API error.
	ErrTransportFailed = errors.New("transport failed")

	// ErrCredentialNotFound indicates that a credential reference specified in
	// the resource configuration does not match any loaded credential.
	ErrCredentialNotFound = errors.New("credential not found")

	// ErrUnsupportedTransport indicates that no transporter is registered for
	// the given resource type.
	ErrUnsupportedTransport = errors.New("unsupported transport type")
)
