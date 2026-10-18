package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fullstacks-gmbh/airgapper/internal/credentials"
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// Check verifies what Validate cannot see from the configuration alone: that
// every credential reference resolves, and that the files the configuration
// points to exist. haveCreds says whether a credentials path was given, so a
// missing --credentials is reported as such instead of as many unknown names.
//
// Every problem is reported, joined into one error wrapped around
// domain.ErrInvalidConfig. The network is never touched. Only credentials that
// resources actually reference are inspected, so an unused entry with a stale
// key path does not fail the check.
func Check(cfg *Config, creds []domain.Credential, haveCreds bool) error {
	store := credentials.NewFileStore(creds)

	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format+": %w", append(args, domain.ErrInvalidConfig)...))
	}

	for i := range cfg.Resources {
		rc := &cfg.Resources[i]
		res := rc.ToResource()
		prefix := fmt.Sprintf("resource[%d]", i)

		for _, ref := range []struct{ field, name string }{
			{"source_credentials_ref", rc.SourceCredentialsRef},
			{"target_credentials_ref", rc.TargetCredentialsRef},
		} {
			if ref.name == "" {
				continue
			}
			if !haveCreds {
				fail("%s: %s %q needs a credentials file (--credentials)", prefix, ref.field, ref.name)
				continue
			}
			cred, err := store.ResolveByRef(ref.name, credentialType(res.Type))
			if err != nil {
				fail("%s: %s: %v", prefix, ref.field, err)
				continue
			}
			if key := cred.SSHKeyPath; key != "" {
				if err := requireFile(key); err != nil {
					fail("%s: %s %q: ssh key: %v", prefix, ref.field, ref.name, err)
				}
			}
		}

		if path := strings.TrimSpace(rc.PolicyPath); path != "" {
			if err := requireFile(path); err != nil {
				fail("%s: policy_path: %v", prefix, err)
			}
		}
		for _, dir := range []struct{ field, path string }{
			{"source_ca_cert", rc.SourceCACert},
			{"destination_ca_cert", rc.DestinationCACert},
		} {
			if path := strings.TrimSpace(dir.path); path != "" {
				if err := requireFile(filepath.Join(path, "ca.crt")); err != nil {
					fail("%s: %s: %v", prefix, dir.field, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// credentialType maps a resource type to the credential section it uses.
func credentialType(t domain.ResourceType) domain.CredentialType {
	switch t {
	case domain.ResourceTypeHelm:
		return domain.CredentialTypeHelm
	case domain.ResourceTypeGit:
		return domain.CredentialTypeGit
	default:
		return domain.CredentialTypeImage
	}
}

// requireFile reports an error unless path is a readable regular file.
func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, expected a file", path)
	}
	return nil
}
