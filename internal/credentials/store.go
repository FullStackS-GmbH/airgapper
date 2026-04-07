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

	// byName indexes credentials by their Name field for fast lookups.
	byName map[string]*domain.Credential
}

// Ensure FileStore implements domain.CredentialStore at compile time.
var _ domain.CredentialStore = (*FileStore)(nil)

// NewFileStore creates a new FileStore from the given credential slice. It
// builds an internal index by credential name. If duplicate names are detected,
// a warning is logged via slog and the last entry wins.
func NewFileStore(creds []domain.Credential) *FileStore {
	fs := &FileStore{
		credentials: make([]domain.Credential, len(creds)),
		byName:      make(map[string]*domain.Credential, len(creds)),
	}

	copy(fs.credentials, creds)

	for i := range fs.credentials {
		c := &fs.credentials[i]
		if _, exists := fs.byName[c.Name]; exists {
			slog.Warn("duplicate credential name; previous entry will be overwritten",
				slog.String("name", c.Name),
				slog.String("type", string(c.Type)),
			)
		}
		fs.byName[c.Name] = c
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
	for i := range fs.credentials {
		c := &fs.credentials[i]
		if c.Name == host && c.Type == credType {
			return c, nil
		}
	}
	return nil, nil
}

// ResolveByRef looks up a credential by its exact reference name, as specified
// in a resource's source_credentials_ref or target_credentials_ref field.
//
// Returns domain.ErrCredentialNotFound if no credential with the given name
// exists.
func (fs *FileStore) ResolveByRef(ref string) (*domain.Credential, error) {
	c, ok := fs.byName[ref]
	if !ok {
		return nil, fmt.Errorf("credential %q: %w", ref, domain.ErrCredentialNotFound)
	}
	return c, nil
}
