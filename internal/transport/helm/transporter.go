package helm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/types"
	"helm.sh/helm/v4/pkg/registry"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/transport"
	transportregistry "github.com/fullstacks-gmbh/airgapper/internal/transport/registry"
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
	logger := opts.Logger
	if logger == nil {
		logger = t.logger
	}

	// syncVersion resolves the effective destination repository (chart name) as
	// it goes, so it takes a pointer and the resolved copy is reported below.
	effectiveResource := resource
	result := transport.SyncVersions(effectiveResource.Versions, func(version string) (domain.VersionResult, []domain.OperationRecord) {
		return t.syncVersion(ctx, &effectiveResource, version, opts, logger)
	})
	result.Resource = effectiveResource

	return result, nil
}

// syncVersion handles the sync logic for a single chart version.
func (t *Transporter) syncVersion(ctx context.Context, resource *domain.Resource, version string, opts domain.SyncOptions, logger *slog.Logger) (domain.VersionResult, []domain.OperationRecord) {
	destination := resource.Destination
	destination.Repository = destinationChartRepository(resource.Destination.Repository, resource.Source.Repository, resource.DestinationChart)

	srcRef := chartRef(resource.Source, version)
	dstRef := NormalizeOCIRef(destination.Registry, destination.Repository, version)

	logger = logger.With("version", version, "src", srcRef)

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
	srcCred, err := transportregistry.ResolveCredentials(resource.SourceCredentialsRef, resource.Source.Registry, domain.CredentialTypeHelm, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve source credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve source credentials: "+err.Error())}
	}

	dstCred, err := transportregistry.ResolveCredentials(resource.TargetCredentialsRef, resource.Destination.Registry, domain.CredentialTypeHelm, opts.Credentials)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("resolve target credentials: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "resolve target credentials: "+err.Error())}
	}

	// settle checks the destination and decides whether the version needs
	// syncing at all: it returns done=true with the final result for a dry run, a
	// skip, or a failed check. pulled records whether the chart was already
	// downloaded, for the operation log.
	settle := func(dest domain.Endpoint, ref string, pulled bool) (exists, done bool, vr domain.VersionResult, ops []domain.OperationRecord) {
		var prior []domain.OperationRecord
		if pulled {
			prior = []domain.OperationRecord{op(domain.OpPull, "pulled from source")}
		}
		// Helm destinations are OCI registries. Force OCI handling for private
		// registry hostnames that are not part of the source auto-detection list.
		if !IsOCIRegistry(dest.Registry) {
			dest.Registry = "oci://" + dest.Registry
		}
		exists, err := t.Exists(ctx, dest, version, dstCred)
		if err != nil {
			checkErr := fmt.Errorf("check destination: %w", err)
			return false, true, domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: checkErr, Message: checkErr.Error()},
				append(prior, op(domain.OpFail, checkErr.Error()))
		}
		if opts.DryRun {
			vr, ops = transport.DryRunResult(resource.PushMode, version, exists, logger, op)
			return exists, true, vr, append(prior, ops...)
		}
		if exists && resource.PushMode == domain.PushModeSkip {
			logger.Info("chart already exists at destination, skipping")
			return exists, true, domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: "already exists"},
				append(prior, op(domain.OpSkip, "already exists"))
		}
		return exists, false, domain.VersionResult{}, nil
	}

	// Decide before downloading, using the destination name known from the
	// configuration, so repeat runs and dry runs do not fetch every chart.
	exists, done, vr, ops := settle(destination, dstRef, false)
	if done {
		return vr, ops
	}

	// Pull chart from source.
	logger.Info("pulling chart from source")
	var chartData []byte
	if IsOCIRegistry(resource.Source.Registry) {
		// Source and destination use separate clients because Helm's Login
		// installs a host-scoped credential callback on the client.
		sourceClient, err := newRegistryClient(ctx, resource.Source, domain.Endpoint{})
		if err != nil {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("create source registry client: %w", err)},
				[]domain.OperationRecord{op(domain.OpFail, "create source registry client: "+err.Error())}
		}
		if srcCred != nil {
			srcHost := extractHost(resource.Source.Registry)
			if loginErr := sourceClient.Login(srcHost, registry.LoginOptBasicAuth(srcCred.Username, srcCred.Password)); loginErr != nil {
				return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("login to source %q: %w", srcHost, loginErr)},
					[]domain.OperationRecord{op(domain.OpFail, "login to source: "+loginErr.Error())}
			}
		}
		pullResult, err := sourceClient.Pull(srcRef)
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

	chartData, targetChart, err := prepareChartArchive(chartData, resource.DestinationChart)
	if err != nil {
		logger.Error("failed to prepare chart", "error", err)
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("prepare chart: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "pulled from source"), op(domain.OpFail, "prepare chart: "+err.Error())}
	}

	checkedRef := dstRef
	destination.Repository = destinationChartRepository(resource.Destination.Repository, resource.Source.Repository, targetChart)
	resource.Destination.Repository = destination.Repository
	dstRef = NormalizeOCIRef(destination.Registry, destination.Repository, version)
	logger = logger.With("dst", dstRef, "chart_name", targetChart)

	// The chart's own name can differ from the one assumed above; check again
	// only when the destination actually changed.
	if dstRef != checkedRef {
		exists, done, vr, ops = settle(destination, dstRef, true)
		if done {
			return vr, ops
		}
	}

	destinationClient, err := newRegistryClient(ctx, domain.Endpoint{}, resource.Destination)
	if err != nil {
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("create destination registry client: %w", err)},
			[]domain.OperationRecord{op(domain.OpFail, "create destination registry client: "+err.Error())}
	}
	if dstCred != nil {
		dstHost := extractHost(resource.Destination.Registry)
		if loginErr := destinationClient.Login(dstHost, registry.LoginOptBasicAuth(dstCred.Username, dstCred.Password)); loginErr != nil {
			return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("login to destination %q: %w", dstHost, loginErr)},
				[]domain.OperationRecord{op(domain.OpFail, "login to destination: "+loginErr.Error())}
		}
	}

	// Push chart to destination. Helm v4 strict mode expects the push target to
	// end in "/{chart-name}:{chart-version}", which dstRef already does.
	logger.Info("pushing chart to destination")
	_, err = destinationClient.Push(chartData, dstRef)
	if err != nil {
		logger.Error("failed to push chart", "error", err)
		return domain.VersionResult{Version: version, Status: domain.SyncStatusFailed, Error: fmt.Errorf("push chart: %w", err)},
			[]domain.OperationRecord{op(domain.OpPull, "pulled from source"), op(domain.OpFail, "push chart: "+err.Error())}
	}

	logger.Info("chart synced successfully")

	ops = []domain.OperationRecord{op(domain.OpPull, "pulled from source")}
	if exists && resource.PushMode == domain.PushModeForce {
		ops = append(ops, op(domain.OpOverwrite, "overwritten at destination"))
	} else {
		ops = append(ops, op(domain.OpPush, "pushed to destination"))
	}
	return domain.VersionResult{Version: version, Status: domain.SyncStatusSynced, Message: "copied"}, ops
}

// PullChartBytes pulls a Helm chart and returns its raw tarball bytes.
// It supports both OCI and legacy HTTP chart repositories.
func (t *Transporter) PullChartBytes(ctx context.Context, resource domain.Resource, version string, credStore domain.CredentialStore) ([]byte, error) {
	srcCred, err := transportregistry.ResolveCredentials(resource.SourceCredentialsRef, resource.Source.Registry, domain.CredentialTypeHelm, credStore)
	if err != nil {
		return nil, fmt.Errorf("resolve source credentials: %w", err)
	}

	if !IsOCIRegistry(resource.Source.Registry) {
		data, _, err := t.pullLegacyChart(ctx, resource.Source, version, srcCred)
		return data, err
	}

	client, err := newRegistryClient(ctx, resource.Source, domain.Endpoint{})
	if err != nil {
		return nil, fmt.Errorf("create registry client: %w", err)
	}

	if srcCred != nil {
		srcHost := extractHost(resource.Source.Registry)
		if loginErr := client.Login(srcHost, registry.LoginOptBasicAuth(srcCred.Username, srcCred.Password)); loginErr != nil {
			return nil, fmt.Errorf("login to source %q: %w", srcHost, loginErr)
		}
	}

	srcRef := chartRef(resource.Source, version)
	pullResult, err := client.Pull(srcRef)
	if err != nil {
		return nil, fmt.Errorf("pull chart: %w", err)
	}
	return pullResult.Chart.Data, nil
}

// Exists checks whether a specific chart version exists at the given endpoint.
// For OCI registries it looks up the manifest; for legacy repositories it
// fetches index.yaml and searches for the chart version.
func (t *Transporter) Exists(ctx context.Context, endpoint domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	if !IsOCIRegistry(endpoint.Registry) {
		return t.legacyChartExists(ctx, endpoint, version, creds)
	}

	// Build an OCI reference for the manifest check.
	refStr := fmt.Sprintf("%s/%s:%s", extractHost(endpoint.Registry), endpoint.Repository, ociTag(version))

	ref, err := transportregistry.ParseRef(refStr)
	if err != nil {
		return false, fmt.Errorf("parse reference %q: %w", refStr, err)
	}

	return transportregistry.ManifestExists(ctx, registrySystemContext(creds, endpoint), ref, t.logger)
}

// ListVersions returns all available versions for a chart at the given
// endpoint. For OCI registries, it lists tags using crane. For legacy
// repositories, it parses index.yaml.
func (t *Transporter) ListVersions(ctx context.Context, endpoint domain.Endpoint, creds *domain.Credential) ([]string, error) {
	if !IsOCIRegistry(endpoint.Registry) {
		return t.listLegacyVersions(ctx, endpoint, creds)
	}

	repo := fmt.Sprintf("%s/%s", extractHost(endpoint.Registry), endpoint.Repository)
	ref, err := transportregistry.ParseRef(repo)
	if err != nil {
		return nil, fmt.Errorf("parse repo %q: %w", repo, err)
	}

	sys := registrySystemContext(creds, endpoint)
	tags, err := docker.GetRepositoryTags(ctx, sys, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags for %q: %w", repo, err)
	}

	for i := range tags {
		tags[i] = strings.ReplaceAll(tags[i], "_", "+")
	}

	return tags, nil
}

// registrySystemContext builds the containers/image context for lookups against
// an OCI endpoint. Local and http:// registries are plain HTTP, as in
// newRegistryClient, so lookups must not insist on TLS for them.
func registrySystemContext(creds *domain.Credential, endpoint domain.Endpoint) *types.SystemContext {
	return transportregistry.SystemContext(creds, endpoint.Insecure || needsPlainHTTP(endpoint.Registry), endpoint.CACertPath)
}

// newRegistryClient builds a Helm registry client for the given endpoints. All
// of its requests follow ctx, see ctxTransport.
func newRegistryClient(ctx context.Context, source, destination domain.Endpoint) (*registry.Client, error) {
	opts := []registry.ClientOption{registry.ClientOptWriter(io.Discard)}
	base := http.RoundTripper(registry.NewTransport(false))

	switch {
	case (IsOCIRegistry(source.Registry) && needsPlainHTTP(source.Registry)) || needsPlainHTTP(destination.Registry):
		opts = append(opts, registry.ClientOptPlainHTTP())
	case source.Insecure || destination.Insecure:
		base = insecureHTTPClient.Transport
	case source.CACertPath != "" || destination.CACertPath != "":
		certDir := source.CACertPath
		if certDir == "" {
			certDir = destination.CACertPath
		}
		client, err := caCertHTTPClient(certDir)
		if err != nil {
			return nil, fmt.Errorf("configure registry client CA cert: %w", err)
		}
		base = client.Transport
	}

	opts = append(opts, registry.ClientOptHTTPClient(&http.Client{Transport: ctxTransport{base: base, ctx: ctx}}))
	return registry.NewClient(opts...)
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

func destinationChartRepository(destinationRepo, sourceChart, destinationChart string) string {
	repo := strings.Trim(destinationRepo, "/")
	name := strings.Trim(destinationChart, "/")
	if name == "" {
		name = chartName(sourceChart)
	}
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

// extractHost removes the "oci://" prefix and any trailing slashes from a
// registry string, returning the hostname and optional port. Unlike hostOf it
// keeps the port, since it feeds registry references and Helm logins.
func extractHost(registry string) string {
	return strings.TrimRight(strings.TrimPrefix(registry, "oci://"), "/")
}
