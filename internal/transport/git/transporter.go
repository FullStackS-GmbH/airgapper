package git

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

// Transporter handles synchronization of Git repositories between remotes. It
// supports HTTPS (with basic auth), SSH (with key files), and Azure Repos
// authentication patterns.
type Transporter struct {
	logger *slog.Logger
}

// New creates a new Git Transporter with the given logger.
func New(logger *slog.Logger) *Transporter {
	return &Transporter{logger: logger}
}

// Type returns domain.ResourceTypeGit.
func (t *Transporter) Type() domain.ResourceType {
	return domain.ResourceTypeGit
}

// Sync copies Git refs from source to destination for each ref listed in the
// resource. It respects the PushMode and DryRun settings.
func (t *Transporter) Sync(ctx context.Context, resource domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	result := &domain.SyncResult{Resource: resource}
	logger := opts.Logger
	if logger == nil {
		logger = t.logger
	}

	for _, ref := range resource.Versions {
		vr := t.syncRef(ctx, resource, ref, opts, logger)
		switch vr.Status {
		case domain.SyncStatusSynced:
			result.Synced = append(result.Synced, vr)
		case domain.SyncStatusSkipped:
			result.Skipped = append(result.Skipped, vr)
		case domain.SyncStatusFailed:
			result.Failed = append(result.Failed, vr)
		}
	}

	return result, nil
}

// syncRef handles the sync logic for a single git ref.
func (t *Transporter) syncRef(ctx context.Context, resource domain.Resource, ref string, opts domain.SyncOptions, logger *slog.Logger) domain.VersionResult {
	logger = logger.With("ref", ref, "src", resource.Source.Repository, "dst", resource.Destination.Repository)

	// Resolve credentials.
	srcCred, err := resolveCredentials(resource.SourceCredentialsRef, resource.Source, domain.CredentialTypeGit, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)}
	}

	dstCred, err := resolveCredentials(resource.TargetCredentialsRef, resource.Destination, domain.CredentialTypeGit, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)}
	}

	// Check if the ref exists at the destination (for skip mode).
	exists, err := t.Exists(ctx, resource.Destination, ref, dstCred)
	if err != nil {
		logger.Warn("failed to check existence at destination", "error", err)
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("ref already exists at destination, skipping")
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusSkipped, Message: "already exists"}
	}

	// Dry-run mode.
	if opts.DryRun {
		logger.Info("dry-run: would sync ref")
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusSkipped, Message: "dry-run"}
	}

	// Clone source repo to a temporary directory, fetching only the target ref.
	tmpDir, err := os.MkdirTemp("", "airgapper-git-*")
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("create temp dir: %w", err)}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcAuth, err := credToTransportAuth(srcCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build source auth: %w", err)}
	}

	logger.Info("cloning source repository")
	cloneOpts := &gogit.CloneOptions{
		URL:           resource.Source.Repository,
		Auth:          srcAuth,
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
		Depth:         0,
		Tags:          gogit.AllTags,
	}

	repo, err := gogit.PlainCloneContext(ctx, tmpDir, false, cloneOpts)
	if err != nil {
		logger.Error("failed to clone source", "error", err)
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("clone source: %w", err)}
	}

	// Add destination as remote "target".
	dstAuth, err := credToTransportAuth(dstCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build destination auth: %w", err)}
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "target",
		URLs: []string{resource.Destination.Repository},
	})
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("add target remote: %w", err)}
	}

	// Push the ref to the target remote.
	logger.Info("pushing ref to destination")
	refSpec := config.RefSpec(fmt.Sprintf("%s:%s", ref, ref))
	pushOpts := &gogit.PushOptions{
		RemoteName: "target",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       dstAuth,
		Force:      resource.PushMode == domain.PushModeForce || resource.PushMode == domain.PushModeOverwrite,
	}

	if err := repo.PushContext(ctx, pushOpts); err != nil {
		logger.Error("failed to push to destination", "error", err)
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("push to destination: %w", err)}
	}

	logger.Info("ref synced successfully")
	return domain.VersionResult{Version: ref, Status: domain.SyncStatusSynced, Message: "pushed"}
}

// Exists checks whether a specific ref exists at the given endpoint by listing
// remote refs without cloning. It returns true if the ref is found.
func (t *Transporter) Exists(_ context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	auth, err := credToTransportAuth(creds)
	if err != nil {
		return false, fmt.Errorf("build auth: %w", err)
	}

	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "probe",
		URLs: []string{endpoint.Repository},
	})

	refs, err := rem.List(&gogit.ListOptions{Auth: auth})
	if err != nil {
		return false, fmt.Errorf("list remote refs: %w", err)
	}

	for _, ref := range refs {
		if ref.Name().String() == version || ref.Name().Short() == version {
			return true, nil
		}
	}

	return false, nil
}

// ListVersions returns all refs available at the given endpoint. The returned
// list contains the full ref names (e.g. "refs/heads/main", "refs/tags/v1.0").
func (t *Transporter) ListVersions(_ context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	auth, err := credToTransportAuth(creds)
	if err != nil {
		return nil, fmt.Errorf("build auth: %w", err)
	}

	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "probe",
		URLs: []string{endpoint.Repository},
	})

	refs, err := rem.List(&gogit.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("list remote refs: %w", err)
	}

	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name().String())
	}

	return names, nil
}

// credToTransportAuth converts a domain Credential to a go-git transport
// AuthMethod. It handles HTTPS basic auth, SSH key auth, and the Azure Repos
// workaround where the username "AzureReposAuthnSucks" signals that the
// password field contains the name of an environment variable holding the
// Authorization header value.
func credToTransportAuth(cred *domain.Credential) (transport.AuthMethod, error) {
	if cred == nil {
		return nil, nil
	}

	// SSH key authentication.
	if cred.SSHKeyPath != "" {
		keys, err := gitssh.NewPublicKeysFromFile("git", cred.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("load SSH key from %q: %w", cred.SSHKeyPath, err)
		}
		return keys, nil
	}

	// Azure Repos workaround: the Password field is an environment variable
	// name that contains the actual auth token / header value.
	if cred.Username == "AzureReposAuthnSucks" {
		envVal := os.Getenv(cred.Password)
		return &http.BasicAuth{
			Username: cred.Username,
			Password: envVal,
		}, nil
	}

	// Standard HTTPS basic auth.
	return &http.BasicAuth{
		Username: cred.Username,
		Password: cred.Password,
	}, nil
}

// resolveCredentials resolves credentials for an endpoint. If credRef is
// non-empty, it uses ResolveByRef. Otherwise, it falls back to Resolve using
// the endpoint registry as the host. A nil store or no matching credential
// results in nil (anonymous access).
func resolveCredentials(credRef string, endpoint domain.Endpoint, credType domain.CredentialType, store domain.CredentialStore) (*domain.Credential, error) {
	if store == nil {
		return nil, nil
	}

	if credRef != "" {
		cred, err := store.ResolveByRef(credRef)
		if err != nil {
			return nil, fmt.Errorf("resolve credential ref %q: %w", credRef, err)
		}
		return cred, nil
	}

	// For git, the "host" is typically the Registry field, but may be empty.
	// Fall back to the Repository URL if Registry is not set.
	host := endpoint.Registry
	if host == "" {
		host = endpoint.Repository
	}

	cred, err := store.Resolve(host, credType)
	if err != nil {
		return nil, fmt.Errorf("resolve credential for host %q: %w", host, err)
	}
	return cred, nil
}
