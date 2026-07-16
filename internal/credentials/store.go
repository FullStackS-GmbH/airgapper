// Package credentials implements the domain.CredentialStore interface, loading
// credentials from YAML files and resolving them by host or reference name.
package credentials

import (
	"fmt"
	"log/slog"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// FileStore is a credential store backed by an in-memory list of credentials
// loaded from YAML files. It implements domain.CredentialStore.
type FileStore struct {
	// credentials is the flat list of all loaded credentials.
	credentials []domain.Credential

	// byKey indexes credentials by name and type for fast lookups. The same
	// name may legitimately be used for image and Helm registry credentials.
	byKey map[credentialKey]*domain.Credential
}

type credentialKey struct {
	name     string
	credType domain.CredentialType
}

// Ensure FileStore implements domain.CredentialStore at compile time.
var _ domain.CredentialStore = (*FileStore)(nil)

// NewFileStore creates a new FileStore from the given credential slice. It
// builds an internal index by credential name and type. If duplicate entries
// with the same name and type are detected, a warning is logged via slog and
// the last entry wins.
func NewFileStore(creds []domain.Credential) *FileStore {
	fs := &FileStore{
		credentials: make([]domain.Credential, len(creds)),
		byKey:       make(map[credentialKey]*domain.Credential, len(creds)),
	}

	copy(fs.credentials, creds)

	for i := range fs.credentials {
		c := &fs.credentials[i]
		key := credentialKey{name: c.Name, credType: c.Type}
		if _, exists := fs.byKey[key]; exists {
			slog.Warn("duplicate credential; previous entry will be overwritten",
				slog.String("name", c.Name),
				slog.String("type", string(c.Type)),
			)
		}
		fs.byKey[key] = c
	}

	return fs
}

// Resolve searches for a credential whose Name matches the given host string
// and whose Type matches credType. This enables host-based credential lookup
// where the credential name is expected to correspond to a registry hostname
// or service identifier.
//
// Returns (nil, nil) if no matching credential is found, indicating that
// anonymous access should be used.
func (fs *FileStore) Resolve(host string, credType domain.CredentialType) (*domain.Credential, error) {
	return fs.byKey[credentialKey{name: host, credType: credType}], nil
}

// ResolveByRef looks up a credential by its exact reference name and type, as
// specified in a resource's source_credentials_ref or target_credentials_ref
// field.
//
// Returns domain.ErrCredentialNotFound if no credential with the given name
// exists.
func (fs *FileStore) ResolveByRef(ref string, credType domain.CredentialType) (*domain.Credential, error) {
	c, ok := fs.byKey[credentialKey{name: ref, credType: credType}]
	if !ok {
		return nil, fmt.Errorf("%s credential %q: %w", credType, ref, domain.ErrCredentialNotFound)
	}
	return c, nil
}
