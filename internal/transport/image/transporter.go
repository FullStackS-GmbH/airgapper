package image

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/transport"
	"github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
)

// Transporter handles synchronization of container images between registries.
// It uses the containers/image library (go.podman.io/image/v5) — the same
// library that powers Skopeo/Podman/Buildah — for registry operations.
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
	logger := opts.Logger
	if logger == nil {
		logger = t.logger
	}

	result := transport.SyncVersions(resource.Versions, func(version string) (domain.VersionResult, []domain.OperationRecord) {
		return t.syncVersion(ctx, resource, version, opts, logger)
	})
	result.Resource = resource

	return result, nil
}

// syncVersion handles the sync logic for a single image version/tag.
func (t *Transporter) syncVersion(ctx context.Context, resource domain.Resource, version string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
	srcRefStr := buildRef(resource.Source, version)
	dstRefStr := buildRef(resource.Destination, version)

	logger = logger.With("version", version, "src", srcRefStr, "dst", dstRefStr)

	op := func(operation domain.OperationType, msg string) domain.OperationRecord {
		return domain.OperationRecord{
			ResourceType: domain.ResourceTypeImage,
			Operation:    operation,
			Source:       srcRefStr,
			Destination:  dstRefStr,
			Version:      version,
			Message:      msg,
		}
	}

	srcCred, err := registry.ResolveCredentials(resource.SourceCredentialsRef, resource.Source.Registry, domain.CredentialTypeImage, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve source credentials: "+err.Error())}
	}

	dstCred, err := registry.ResolveCredentials(resource.TargetCredentialsRef, resource.Destination.Registry, domain.CredentialTypeImage, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve target credentials: "+err.Error())}
	}

	exists, err := t.Exists(ctx, resource.Destination, version, dstCred)
	if err != nil {
		logger.Warn("failed to check existence at destination", "error", err)
	}

	if opts.DryRun {
		return transport.DryRunResult(resource.PushMode, version, exists, logger, op)
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("image already exists at destination, skipping")
		return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: "already exists"},
			[]domain.OperationRecord{op(domain.OpSkip, "already exists")}
	}

	srcRef, err := registry.ParseRef(srcRefStr)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: err},
			[]domain.OperationRecord{op(domain.OpFail, err.Error())}
	}
	dstRef, err := registry.ParseRef(dstRefStr)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: err},
			[]domain.OperationRecord{op(domain.OpFail, err.Error())}
	}

	policyCtx, err := registry.PermissivePolicyContext()
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: err},
			[]domain.OperationRecord{op(domain.OpFail, err.Error())}
	}

	logger.Info("copying image")
	_, err = copy.Image(ctx, policyCtx, dstRef, srcRef, &copy.Options{
		SourceCtx:          registry.SystemContext(srcCred, false),
		DestinationCtx:     registry.SystemContext(dstCred, false),
		ImageListSelection: copy.CopyAllImages,
		ReportWriter:       io.Discard,
	})
	if err != nil {
		logger.Error("failed to copy image", "error", err)
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("copy image: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "pull from source"), op(domain.OpFail, "copy failed: "+err.Error())}
	}

	logger.Info("image copied successfully")

	var ops []domain.OperationRecord
	ops = append(ops, op(domain.OpPull, "pulled from source"))
	if exists && resource.PushMode == domain.PushModeForce {
		ops = append(ops, op(domain.OpOverwrite, "force pushed (overwritten)"))
	} else {
		ops = append(ops, op(domain.OpPush, "pushed to destination"))
	}
	return domain.VersionResult{Version: version, Status: domain.SyncStatusSynced, Message: "copied"}, ops
}

// Exists checks whether a specific image tag exists at the given endpoint. An
// absent tag yields (false, nil); a credential or rate-limit failure is
// returned so the caller can report it. See registry.ManifestExists.
func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	refStr := buildRef(endpoint, version)
	ref, err := registry.ParseRef(refStr)
	if err != nil {
		return false, fmt.Errorf("parse reference %q: %w", refStr, err)
	}

	return registry.ManifestExists(ctx, registry.SystemContext(creds, false), ref, t.logger)
}

// ListVersions returns all tags available at the given endpoint.
func (t *Transporter) ListVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	repo := endpoint.String()
	ref, err := registry.ParseRef(repo)
	if err != nil {
		return nil, fmt.Errorf("parse repo %q: %w", repo, err)
	}

	sys := registry.SystemContext(creds, false)
	tags, err := docker.GetRepositoryTags(ctx, sys, ref)
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
