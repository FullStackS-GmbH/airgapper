package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

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

	run := &syncRun{}
	defer run.close()

	result := airtransport.SyncVersions(resource.Versions, func(ref string) (domain.VersionResult, []domain.OperationRecord) {
		return t.syncRef(ctx, run, resource, ref, opts, logger)
	})
	result.Resource = resource

	return result, nil
}

// syncRun holds state shared by the refs of one resource: the source ref list
// and a single bare working repository, so the source is not cloned once per
// ref. Objects fetched for one ref are reused by the next.
type syncRun struct {
	srcRefs   []*plumbing.Reference
	srcListed bool
	dir       string
	repo      *gogit.Repository
}

// close removes the working repository, if one was created.
func (r *syncRun) close() {
	if r.dir != "" {
		_ = os.RemoveAll(r.dir)
	}
}

// workRepo returns the working repository, creating it on first use with the
// source as remote "origin" and the destination as remote "target".
func (r *syncRun) workRepo(resource domain.Resource) (*gogit.Repository, error) {
	if r.repo != nil {
		return r.repo, nil
	}
	dir, err := os.MkdirTemp("", "airgapper-git-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	r.dir = dir
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		return nil, fmt.Errorf("init working repository: %w", err)
	}
	for name, url := range map[string]string{"origin": resource.Source.Repository, "target": resource.Destination.Repository} {
		if _, err := repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}}); err != nil {
			return nil, fmt.Errorf("add %s remote: %w", name, err)
		}
	}
	r.repo = repo
	return repo, nil
}

// resolveRef turns a configured ref into a full ref name. Names starting with
// "refs/" are used as they are. A short name such as "main" or "v1.0" resolves
// to the branch of that name, else the tag, as git does.
func (r *syncRun) resolveRef(ctx context.Context, t *Transporter, endpoint domain.Endpoint, ref string, creds *domain.Credential) (string, error) {
	if strings.HasPrefix(ref, "refs/") {
		return ref, nil
	}
	if !r.srcListed {
		refs, err := t.listRefs(ctx, endpoint, creds)
		if err != nil {
			return "", fmt.Errorf("list source refs: %w", err)
		}
		r.srcRefs, r.srcListed = refs, true
	}
	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		for _, candidate := range r.srcRefs {
			if candidate.Name().String() == prefix+ref {
				return candidate.Name().String(), nil
			}
		}
	}
	return "", fmt.Errorf("ref %q not found at source: %w", ref, domain.ErrNotFound)
}

// syncRef handles the sync logic for a single git ref.
func (t *Transporter) syncRef(ctx context.Context, run *syncRun, resource domain.Resource, ref string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
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

	// checkDestination looks the ref up at the destination. A short name matches
	// a branch or a tag; the exact full name is used once it is known.
	checkDestination := func(name string) (bool, *domain.VersionResult, []domain.OperationRecord) {
		exists, err := t.Exists(ctx, resource.Destination, name, dstCred)
		if err != nil {
			checkErr := fmt.Errorf("check destination: %w", err)
			return false, &domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: checkErr, Message: checkErr.Error()},
				[]domain.OperationRecord{op(domain.OpFail, checkErr.Error())}
		}
		return exists, nil, nil
	}

	// Dry-run mode: report what would happen without mutating. It needs only the
	// destination, so the source is not contacted.
	if opts.DryRun {
		exists, failed, ops := checkDestination(ref)
		if failed != nil {
			return *failed, ops
		}
		return airtransport.DryRunResult(resource.PushMode, ref, exists, logger, op)
	}

	// Resolve short names ("main", "v1.0") to full ref names at the source.
	fullRef, err := run.resolveRef(ctx, t, resource.Source, ref, srcCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: err, Message: err.Error()},
			[]domain.OperationRecord{op(domain.OpFail, err.Error())}
	}

	// Check if the ref exists at the destination (for skip mode).
	exists, failed, ops := checkDestination(fullRef)
	if failed != nil {
		return *failed, ops
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("ref already exists at destination, skipping")
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusSkipped, Message: "already exists"},
			[]domain.OperationRecord{op(domain.OpSkip, "already exists")}
	}

	srcAuth, err := credToTransportAuth(srcCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build source auth: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "build source auth: "+err.Error())}
	}
	dstAuth, err := credToTransportAuth(dstCred)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("build destination auth: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "build destination auth: "+err.Error())}
	}

	repo, err := run.workRepo(resource)
	if err != nil {
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: err},
			[]domain.OperationRecord{op(domain.OpFail, err.Error())}
	}

	// Fetch only the target ref into the shared working repository.
	logger.Info("fetching ref from source repository")
	refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", fullRef, fullRef))
	fetchErr := repo.FetchContext(ctx, &gogit.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       srcAuth,
		Tags:       gogit.NoTags,
	})
	if fetchErr != nil && !errors.Is(fetchErr, gogit.NoErrAlreadyUpToDate) {
		logger.Error("failed to fetch from source", "error", fetchErr)
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("fetch source: %w", fetchErr)},
			[]domain.OperationRecord{op(domain.OpFail, "fetch source: "+fetchErr.Error())}
	}

	// Push the ref to the target remote.
	logger.Info("pushing ref to destination")
	pushOpts := &gogit.PushOptions{
		RemoteName: "target",
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", fullRef, fullRef))},
		Auth:       dstAuth,
		Force:      resource.PushMode == domain.PushModeForce,
	}

	// An unchanged ref is not a failure: the destination already holds it.
	if err := repo.PushContext(ctx, pushOpts); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		logger.Error("failed to push to destination", "error", err)
		return domain.VersionResult{Version: ref, Status: domain.SyncStatusFailed, Error: fmt.Errorf("push to destination: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "fetched from source"), op(domain.OpFail, "push to destination: "+err.Error())}
	}

	logger.Info("ref synced successfully")

	ops = []domain.OperationRecord{op(domain.OpPull, "fetched from source")}
	if exists && resource.PushMode == domain.PushModeForce {
		ops = append(ops, op(domain.OpOverwrite, "force pushed (overwritten)"))
	} else {
		ops = append(ops, op(domain.OpPush, "pushed to destination"))
	}
	return domain.VersionResult{Version: ref, Status: domain.SyncStatusSynced, Message: "pushed"}, ops
}

// listRefs lists the refs of a remote without cloning it.
func (t *Transporter) listRefs(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]*plumbing.Reference, error) {
	auth, err := credToTransportAuth(creds)
	if err != nil {
		return nil, fmt.Errorf("build auth: %w", err)
	}

	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "probe",
		URLs: []string{endpoint.Repository},
	})

	refs, err := rem.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list remote refs: %w", err)
	}
	return refs, nil
}

// Exists checks whether a specific ref exists at the given endpoint by listing
// remote refs without cloning. It returns true if the ref is found.
func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	refs, err := t.listRefs(ctx, endpoint, creds)
	if err != nil {
		return false, err
	}

	for _, ref := range refs {
		if ref.Name().String() == version || ref.Name().Short() == version {
			return true, nil
		}
	}

	return false, nil
}

// ListVersions returns the branches and tags available at the given endpoint
// by short name (e.g. "main", "v1.0"), the form used in the configuration. A
// branch and a tag with the same name are listed once; Sync resolves such a
// name to the branch.
func (t *Transporter) ListVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	refs, err := t.listRefs(ctx, endpoint, creds)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(refs))
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		name := ref.Name()
		if !name.IsBranch() && !name.IsTag() || strings.HasSuffix(name.String(), "^{}") {
			continue
		}
		if short := name.Short(); !seen[short] {
			seen[short] = true
			names = append(names, short)
		}
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
