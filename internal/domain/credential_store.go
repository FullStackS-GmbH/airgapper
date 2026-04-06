package domain

// CredentialStore loads and resolves credentials for registry and host access.
// Implementations must never log credential values, even at DEBUG level.
type CredentialStore interface {
	// Resolve finds credentials matching the given host and credential type.
	// It returns (nil, nil) when no matching credential exists, indicating
	// anonymous access should be used.
	Resolve(host string, credType CredentialType) (*Credential, error)

	// ResolveByRef finds credentials by their reference name as specified in
	// the resource configuration. It returns ErrCredentialNotFound if the
	// reference does not exist.
	ResolveByRef(ref string) (*Credential, error)
}
