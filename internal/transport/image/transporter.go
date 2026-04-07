package image

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

// Transporter handles synchronization of container images between registries.
// It uses the go-containerregistry library (the same library that powers
// crane and ko) for pure-Go, CGO-free registry operations.
type Transporter struct {
	logger *slog.Logger
}

// New creates a new image Transporter with the given logger.
func New(logger *slog.Logger) *Transporter {
	return &Transporter{logger: logger}
}

// Type returns domain.ResourceTypeImage.
func (t *Transporter) Type() domain.ResourceType {
	return domain.ResourceTypeImage
}

// Sync copies container images from source to destination for each version
// listed in the resource. It respects the PushMode and DryRun settings.
func (t *Transporter) Sync(ctx context.Context, resource domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	result := &domain.SyncResult{Resource: resource}
	logger := opts.Logger
	if logger == nil {
		logger = t.logger
	}

	for _, version := range resource.Versions {
		vr, ops := t.syncVersion(ctx, resource, version, opts, logger)
		result.Operations = append(result.Operations, ops...)
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

// syncVersion handles the sync logic for a single image version/tag.
func (t *Transporter) syncVersion(ctx context.Context, resource domain.Resource, version string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
	srcRef := buildRef(resource.Source, version)
	dstRef := buildRef(resource.Destination, version)

	logger = logger.With("version", version, "src", srcRef, "dst", dstRef)

	op := func(operation domain.OperationType, msg string) domain.OperationRecord {
		return domain.OperationRecord{
			ResourceType: domain.ResourceTypeImage,
			Operation:    operation,
			Source:       srcRef,
			Destination:  dstRef,
			Version:      version,
			Message:      msg,
		}
	}

	// Resolve credentials.
	srcCred, err := resolveCredentials(resource.SourceCredentialsRef, resource.Source, domain.CredentialTypeImage, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve source credentials: "+err.Error())}
	}

	dstCred, err := resolveCredentials(resource.TargetCredentialsRef, resource.Destination, domain.CredentialTypeImage, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve target credentials: "+err.Error())}
	}

	// Check if the image already exists at the destination.
	exists, err := t.Exists(ctx, resource.Destination, version, dstCred)
	if err != nil {
		logger.Warn("failed to check existence at destination", "error", err)
		// Non-fatal: proceed with the copy attempt.
	}

	// Dry-run mode: report what would happen without mutating.
	if opts.DryRun {
		return t.dryRunResult(resource, version, exists, logger, op)
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("image already exists at destination, skipping")
		return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: "already exists"},
			[]domain.OperationRecord{op(domain.OpSkip, "already exists")}
	}

	// Build crane options with authentication.
	craneOpts := []crane.Option{crane.WithContext(ctx)}
	srcAuth := credToAuth(srcCred)
	dstAuth := credToAuth(dstCred)

	craneOpts = append(craneOpts, crane.WithAuthFromKeychain(keychainFunc(func(res authn.Resource) (authn.Authenticator, error) {
		// Determine which auth to use based on the registry being accessed.
		srcReg, _ := ParseImageRef(srcRef)
		if res.RegistryStr() == srcReg {
			return srcAuth, nil
		}
		return dstAuth, nil
	})))

	// Copy the image from source to destination.
	logger.Info("copying image")
	if err := crane.Copy(srcRef, dstRef, craneOpts...); err != nil {
		logger.Error("failed to copy image", "error", err)
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("copy image: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "pull from source"), op(domain.OpFail, "copy failed: "+err.Error())}
	}

	logger.Info("image copied successfully")

	var ops []domain.OperationRecord
	ops = append(ops, op(domain.OpPull, "pulled from source"))
	if exists && (resource.PushMode == domain.PushModeForce || resource.PushMode == domain.PushModeOverwrite) {
		ops = append(ops, op(domain.OpForce, "force pushed (overwritten)"))
	} else {
		ops = append(ops, op(domain.OpPush, "pushed to destination"))
	}
	return domain.VersionResult{Version: version, Status: domain.SyncStatusSynced, Message: "copied"}, ops
}

// dryRunResult returns the appropriate dry-run result based on existence and push mode.
func (t *Transporter) dryRunResult(resource domain.Resource, version string, exists bool, logger *slog.Logger, op func(domain.OperationType, string) domain.OperationRecord) (domain.VersionResult, []domain.OperationRecord) {
	var msg string
	var opRec domain.OperationRecord

	switch {
	case exists && resource.PushMode == domain.PushModeSkip:
		msg = "dry-run: would skip (already exists)"
		opRec = op(domain.OpSkip, msg)
	case exists:
		msg = "dry-run: would overwrite (already exists)"
		opRec = op(domain.OpOverwrite, msg)
	default:
		msg = "dry-run: would sync"
		opRec = op(domain.OpPush, msg)
	}

	logger.Info(msg)
	return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: msg},
		[]domain.OperationRecord{opRec}
}

// Exists checks whether a specific image tag exists at the given endpoint.
// It returns false on 404 and true otherwise.
func (t *Transporter) Exists(_ context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	ref := buildRef(endpoint, version)

	parsed, err := name.ParseReference(ref)
	if err != nil {
		return false, fmt.Errorf("parse reference %q: %w", ref, err)
	}

	auth := credToAuth(creds)
	_, err = remote.Head(parsed, remote.WithAuth(auth))
	if err != nil {
		// Treat any error as "not found" for existence checks. In practice,
		// go-containerregistry returns a transport error with status 404 when
		// the manifest is not found.
		return false, nil
	}

	return true, nil
}

// ListVersions returns all tags available at the given endpoint.
func (t *Transporter) ListVersions(_ context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	repo := endpoint.String()

	auth := credToAuth(creds)
	tags, err := crane.ListTags(repo, crane.WithAuth(auth))
	if err != nil {
		return nil, fmt.Errorf("list tags for %q: %w", repo, err)
	}

	return tags, nil
}

// buildRef constructs a full image reference string from an endpoint and version.
// The format is "registry/repository:version".
func buildRef(ep domain.Endpoint, version string) string {
	base := ep.String()
	return base + ":" + version
}

// credToAuth converts a domain Credential to an authn.Authenticator suitable
// for go-containerregistry operations. If the credential is nil, anonymous
// access is used.
func credToAuth(cred *domain.Credential) authn.Authenticator {
	if cred == nil {
		return authn.Anonymous
	}
	return &authn.Basic{Username: cred.Username, Password: cred.Password}
}

// resolveCredentials resolves credentials for an endpoint. If credRef is
// non-empty, it uses ResolveByRef for an explicit credential reference.
// Otherwise, it falls back to Resolve using the endpoint registry as the host.
// A nil store or no matching credential results in nil (anonymous access).
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

	cred, err := store.Resolve(endpoint.Registry, credType)
	if err != nil {
		return nil, fmt.Errorf("resolve credential for host %q: %w", endpoint.Registry, err)
	}
	return cred, nil
}

// keychainFunc adapts a function to the authn.Keychain interface so that
// different authenticators can be returned depending on the target registry.
type keychainFunc func(authn.Resource) (authn.Authenticator, error)

// Resolve implements authn.Keychain.
func (f keychainFunc) Resolve(r authn.Resource) (authn.Authenticator, error) {
	return f(r)
}
