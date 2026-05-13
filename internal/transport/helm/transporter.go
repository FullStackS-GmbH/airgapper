package helm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"helm.sh/helm/v4/pkg/registry"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// Transporter handles synchronization of Helm charts between registries. It
// supports both OCI-compliant registries (using the Helm registry client) and
// legacy HTTP chart repositories.
type Transporter struct {
	logger *slog.Logger
}

// New creates a new Helm chart Transporter with the given logger.
func New(logger *slog.Logger) *Transporter {
	return &Transporter{logger: logger}
}

// Type returns domain.ResourceTypeHelm.
func (t *Transporter) Type() domain.ResourceType {
	return domain.ResourceTypeHelm
}

// Sync copies Helm charts from source to destination for each version listed
// in the resource. It respects the PushMode and DryRun settings.
func (t *Transporter) Sync(ctx context.Context, resource domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	effectiveResource := resource
	effectiveResource.Destination.Repository = destinationChartRepository(resource.Destination.Repository, resource.Source.Repository)
	result := &domain.SyncResult{Resource: effectiveResource}
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

// syncVersion handles the sync logic for a single chart version.
func (t *Transporter) syncVersion(ctx context.Context, resource domain.Resource, version string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
	destination := resource.Destination
	destination.Repository = destinationChartRepository(resource.Destination.Repository, resource.Source.Repository)

	srcRef := chartRef(resource.Source, version)
	dstRef := NormalizeOCIRef(destination.Registry, destination.Repository, version)

	logger = logger.With("version", version, "src", srcRef, "dst", dstRef)

	op := func(operation domain.OperationType, msg string) domain.OperationRecord {
		return domain.OperationRecord{
			ResourceType: domain.ResourceTypeHelm,
			Operation:    operation,
			Source:       srcRef,
			Destination:  dstRef,
			Version:      version,
			Message:      msg,
		}
	}

	// Resolve credentials.
	srcCred, err := resolveCredentials(resource.SourceCredentialsRef, resource.Source, domain.CredentialTypeHelm, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve source credentials: "+err.Error())}
	}

	dstCred, err := resolveCredentials(resource.TargetCredentialsRef, resource.Destination, domain.CredentialTypeHelm, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve target credentials: "+err.Error())}
	}

	// Check if the chart already exists at the destination.
	exists, err := t.Exists(ctx, destination, version, dstCred)
	if err != nil {
		logger.Warn("failed to check existence at destination", "error", err)
	}

	// Dry-run mode: report what would happen without mutating.
	if opts.DryRun {
		return t.dryRunResult(resource, version, exists, logger, op)
	}

	if exists && resource.PushMode == domain.PushModeSkip {
		logger.Info("chart already exists at destination, skipping")
		return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: "already exists"},
			[]domain.OperationRecord{op(domain.OpSkip, "already exists")}
	}

	// Create a Helm registry client for OCI pull/push operations.
	client, err := newRegistryClient(resource.Source.Registry, resource.Destination.Registry)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("create registry client: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "create registry client: "+err.Error())}
	}

	// Login to source registry.
	if srcCred != nil && IsOCIRegistry(resource.Source.Registry) {
		srcHost := extractHost(resource.Source.Registry)
		if loginErr := client.Login(srcHost, registry.LoginOptBasicAuth(srcCred.Username, srcCred.Password)); loginErr != nil {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("login to source %q: %w", srcHost, loginErr)},
				[]domain.OperationRecord{op(domain.OpFail, "login to source: "+loginErr.Error())}
		}
	}

	// Login to destination registry.
	if dstCred != nil {
		dstHost := extractHost(resource.Destination.Registry)
		if loginErr := client.Login(dstHost, registry.LoginOptBasicAuth(dstCred.Username, dstCred.Password)); loginErr != nil {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("login to destination %q: %w", dstHost, loginErr)},
				[]domain.OperationRecord{op(domain.OpFail, "login to destination: "+loginErr.Error())}
		}
	}

	// Pull chart from source.
	logger.Info("pulling chart from source")
	var chartData []byte
	if IsOCIRegistry(resource.Source.Registry) {
		pullResult, err := client.Pull(srcRef)
		if err != nil {
			logger.Error("failed to pull chart", "error", err)
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("pull chart: %w", err)},
				[]domain.OperationRecord{op(domain.OpFail, "pull chart: "+err.Error())}
		}
		chartData = pullResult.Chart.Data
	} else {
		var err error
		chartData, srcRef, err = t.pullLegacyChart(ctx, resource.Source, version, srcCred)
		if err != nil {
			logger.Error("failed to pull chart", "error", err)
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("pull chart: %w", err)},
				[]domain.OperationRecord{op(domain.OpFail, "pull chart: "+err.Error())}
		}
	}

	// Helm v4 strict mode expects the push target to end in
	// "/{chart-name}:{chart-version}".
	dstPushRef := dstRef

	// Push chart to destination.
	logger.Info("pushing chart to destination")
	_, err = client.Push(chartData, dstPushRef)
	if err != nil {
		logger.Error("failed to push chart", "error", err)
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("push chart: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "pulled from source"), op(domain.OpFail, "push chart: "+err.Error())}
	}

	logger.Info("chart synced successfully")

	var ops []domain.OperationRecord
	ops = append(ops, op(domain.OpPull, "pulled from source"))
	if exists && (resource.PushMode == domain.PushModeForce || resource.PushMode == domain.PushModeOverwrite) {
		ops = append(ops, op(domain.OpOverwrite, "overwritten at destination"))
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

// Exists checks whether a specific chart version exists at the given endpoint.
// For OCI registries, it checks for the OCI manifest using remote.Head. For
// legacy repositories, it fetches index.yaml and searches for the chart
// version.
func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	if !IsOCIRegistry(endpoint.Registry) {
		return t.legacyChartExists(ctx, endpoint, version, creds)
	}

	// Build an OCI reference for the manifest check.
	ref := fmt.Sprintf("%s/%s:%s", extractHost(endpoint.Registry), endpoint.Repository, ociTag(version))

	parsed, err := name.ParseReference(ref)
	if err != nil {
		return false, fmt.Errorf("parse reference %q: %w", ref, err)
	}

	auth := helmCredToAuth(creds)
	_, err = remote.Head(parsed, remote.WithAuth(auth))
	if err != nil {
		return false, nil
	}

	return true, nil
}

// ListVersions returns all available versions for a chart at the given
// endpoint. For OCI registries, it lists tags using crane. For legacy
// repositories, it parses index.yaml.
func (t *Transporter) ListVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	if !IsOCIRegistry(endpoint.Registry) {
		return t.listLegacyVersions(ctx, endpoint, creds)
	}

	repo := fmt.Sprintf("%s/%s", extractHost(endpoint.Registry), endpoint.Repository)
	auth := helmCredToAuth(creds)

	tags, err := crane.ListTags(repo, crane.WithAuth(auth))
	if err != nil {
		return nil, fmt.Errorf("list tags for %q: %w", repo, err)
	}

	for i := range tags {
		tags[i] = strings.ReplaceAll(tags[i], "_", "+")
	}

	return tags, nil
}

func newRegistryClient(sourceRegistry, destinationRegistry string) (*registry.Client, error) {
	if (IsOCIRegistry(sourceRegistry) && needsPlainHTTP(sourceRegistry)) || needsPlainHTTP(destinationRegistry) {
		return registry.NewClient(registry.ClientOptWriter(io.Discard), registry.ClientOptPlainHTTP())
	}
	return registry.NewClient(registry.ClientOptWriter(io.Discard))
}

func chartRef(endpoint domain.Endpoint, version string) string {
	if IsOCIRegistry(endpoint.Registry) {
		return NormalizeOCIRef(endpoint.Registry, endpoint.Repository, version)
	}

	base := legacyRepoBaseURL(endpoint.Registry)
	if version == "" {
		return fmt.Sprintf("%s/%s", base, endpoint.Repository)
	}
	return fmt.Sprintf("%s/%s:%s", base, endpoint.Repository, version)
}

func destinationChartRepository(destinationRepo, sourceChart string) string {
	repo := strings.Trim(destinationRepo, "/")
	name := chartName(sourceChart)
	if name == "" {
		return repo
	}
	if repo == "" {
		return name
	}
	if chartName(repo) == name {
		return repo
	}
	return repo + "/" + name
}

func chartName(repository string) string {
	repository = strings.Trim(repository, "/")
	if repository == "" {
		return ""
	}
	if idx := strings.LastIndex(repository, "/"); idx != -1 {
		return repository[idx+1:]
	}
	return repository
}

func ociTag(version string) string {
	return strings.ReplaceAll(version, "+", "_")
}

// helmCredToAuth converts a domain Credential to an authn.Authenticator.
func helmCredToAuth(cred *domain.Credential) authn.Authenticator {
	if cred == nil {
		return authn.Anonymous
	}
	return &authn.Basic{Username: cred.Username, Password: cred.Password}
}

// resolveCredentials resolves credentials for an endpoint. If credRef is
// non-empty, it uses ResolveByRef. Otherwise, it falls back to Resolve using
// the endpoint registry as the host.
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

// extractHost removes the "oci://" prefix and any trailing path from a
// registry string, returning just the hostname (and optional port).
func extractHost(registry string) string {
	host := registry
	if len(host) > 6 && host[:6] == "oci://" {
		host = host[6:]
	}
	// Remove trailing slashes.
	for len(host) > 0 && host[len(host)-1] == '/' {
		host = host[:len(host)-1]
	}
	return host
}
