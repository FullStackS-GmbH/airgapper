package registry

import (
	"fmt"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// ResolveCredentials resolves credentials for a transport operation. If credRef
// is non-empty it is looked up by reference; otherwise host is used for a
// host-based lookup. A nil store or no matching credential yields nil, nil
// (anonymous access).
func ResolveCredentials(credRef, host string, credType domain.CredentialType, store domain.CredentialStore) (*domain.Credential, error) {
	if store == nil {
		return nil, nil
	}

	if credRef != "" {
		cred, err := store.ResolveByRef(credRef, credType)
		if err != nil {
			return nil, fmt.Errorf("resolve credential ref %q: %w", credRef, err)
		}
		return cred, nil
	}

	cred, err := store.Resolve(host, credType)
	if err != nil {
		return nil, fmt.Errorf("resolve credential for host %q: %w", host, err)
	}
	return cred, nil
}
