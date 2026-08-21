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

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	airtransport "github.com/fullstacks-gmbh/airgapper/internal/transport"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
)

// azureReposAuthSentinel is a magic username signalling the Azure Repos HTTPS
// workaround: the credential's Password field holds the name of an environment
// variable that contains the actual Authorization value.
const azureReposAuthSentinel = "AzureReposAuthnSucks"

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
	logger := opts.Logger
	if logger == nil {
		logger = t.logger
	}

	result := airtransport.SyncVersions(resource.Versions, func(ref string) (domain.VersionResult, []domain.OperationRecord) {
		return t.syncRef(ctx, resource, ref, opts, logger)
	})
	result.Resource = resource

	return result, nil
}

// syncRef handles the sync logic for a single git ref.
func (t *Transporter) syncRef(ctx context.Context, resource domain.Resource, ref string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
	logger = logger.With("ref", ref, "src", resource.Source.Repository, "dst", resource.Destination.Repository)

	op := func(operation domain.OperationType, msg string) domain.OperationRecord {
		return domain.OperationRecord{
			ResourceType: domain.ResourceTypeGit,
			Operation:    operation,
			Source:       resource.Source.Repository,
			Destination:  resource.Destination.Repository,
			Version:      ref,
			Message:      msg,
		}
	}

	// Resolve credentials.
	srcCred, err := registry.ResolveCredentials(resource.SourceCredentialsRef, gitHost(resource.Source), domain.CredentialTypeGit, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve source credentials: "+err.Error())}
	}

	dstCred, err := registry.ResolveCredentials(resource.TargetCredentialsRef, gitHost(resource.Destination), domain.CredentialTypeGit, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve target credentials: "+err.Error())}
	}

	// Check if the ref exists at the destination (for skip mode).
	exists, err := t.Exists(ctx, resource.Destination, ref, dstCred)
	if err != nil {
		logger.Warn("failed to check existence at destination", "error", err)
	}

	// Dry-run mode: report what would happen without mutating.
	if opts.DryRun {
		return airtransport.DryRunResult(resource.PushMode, ref, exists, logger, op)
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("ref already exists at destination, skipping")
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusSkipped, Message: "already exists"},
			[]domain.OperationRecord{op(domain.OpSkip, "already exists")}
	}

	// Clone source repo to a temporary directory, fetching only the target ref.
	tmpDir, err := os.MkdirTemp("", "airgapper-git-*")
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("create temp dir: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "create temp dir: "+err.Error())}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcAuth, err := credToTransportAuth(srcCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build source auth: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "build source auth: "+err.Error())}
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
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("clone source: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "clone source: "+err.Error())}
	}

	// Add destination as remote "target".
	dstAuth, err := credToTransportAuth(dstCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build destination auth: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "build destination auth: "+err.Error())}
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "target",
		URLs: []string{resource.Destination.Repository},
	})
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("add target remote: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "add target remote: "+err.Error())}
	}

	// Push the ref to the target remote.
	logger.Info("pushing ref to destination")
	refSpec := config.RefSpec(fmt.Sprintf("%s:%s", ref, ref))
	pushOpts := &gogit.PushOptions{
		RemoteName: "target",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       dstAuth,
		Force:      resource.PushMode == domain.PushModeForce,
	}

	if err := repo.PushContext(ctx, pushOpts); err != nil {
		logger.Error("failed to push to destination", "error", err)
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("push to destination: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "cloned from source"), op(domain.OpFail, "push to destination: "+err.Error())}
	}

	logger.Info("ref synced successfully")

	var ops []domain.OperationRecord
	ops = append(ops, op(domain.OpPull, "cloned from source"))
	if exists && resource.PushMode == domain.PushModeForce {
		ops = append(ops, op(domain.OpOverwrite, "force pushed (overwritten)"))
	} else {
		ops = append(ops, op(domain.OpPush, "pushed to destination"))
	}
	return domain.VersionResult{Version: ref, Status: domain.SyncStatusSynced, Message: "pushed"}, ops
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
	if cred.Username == azureReposAuthSentinel {
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

// gitHost returns the lookup key for host-based credential resolution. For git
// the Registry field is typically empty, so it falls back to the Repository URL.
func gitHost(endpoint domain.Endpoint) string {
	if endpoint.Registry != "" {
		return endpoint.Registry
	}
	return endpoint.Repository
}
