// Package sync provides the orchestration engine that drives the artifact
// synchronization workflow. It coordinates transporters, scanners, and
// credential resolution across all configured resources.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	gosync "sync"

	"golang.org/x/sync/errgroup"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/pattern"
)

// maxConcurrency is the maximum number of resources processed in parallel.
const maxConcurrency = 4

// Engine orchestrates the sync workflow across all resources. It resolves
// transporters, expands version patterns, runs optional scanners, and
// delegates the actual copy to the appropriate transporter.
type Engine struct {
	// transporters maps each resource type to its transporter.
	transporters map[domain.ResourceType]domain.Transporter

	// scanners maps scanner names to their implementations.
	scanners map[string]domain.Scanner

	// logger is the structured logger for the engine.
	logger *slog.Logger
}

// NewEngine creates a sync engine from the given transporters, scanner map, and
// logger. Each transporter is keyed by its Type(); if two share a type, the
// last one wins.
func NewEngine(transporters []domain.Transporter, scanners map[string]domain.Scanner, logger *slog.Logger) *Engine {
	byType := make(map[domain.ResourceType]domain.Transporter, len(transporters))
	for _, t := range transporters {
		byType[t.Type()] = t
	}
	return &Engine{
		transporters: byType,
		scanners:     scanners,
		logger:       logger,
	}
}

// Run processes all resources and returns aggregated results. Resources are
// processed concurrently using an errgroup with a maximum of 4 goroutines.
// The method returns all collected SyncResults even if individual resources
// encounter errors. A top-level error is returned only if the context is
// cancelled or an unrecoverable infrastructure error occurs.
func (e *Engine) Run(ctx context.Context, resources []domain.Resource, opts domain.SyncOptions) ([]domain.SyncResult, error) {
	var (
		mu      gosync.Mutex
		results []domain.SyncResult
		g       errgroup.Group
	)

	g.SetLimit(maxConcurrency)

	for _, res := range resources {
		g.Go(func() error {
			result, err := e.processResource(ctx, res, opts)
			if err != nil {
				// A resource-level failure must not abort the rest of the run.
				// An operator mirroring 40 resources needs the other 39 synced
				// and a report naming the one that broke — not a run that stops
				// at the first unreachable registry.
				e.logger.Error("resource failed",
					slog.String("resource_type", res.Type.String()),
					slog.String("source", res.Source.String()),
					slog.String("error", err.Error()),
				)
				result = failedResource(res, err)
			}

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()

			return nil
		})
	}

	// No goroutine returns an error; every failure is recorded as a result.
	_ = g.Wait()

	// A cancelled or timed-out context is a run-level problem: the results
	// gathered so far are still returned, but the caller must not treat the run
	// as complete.
	if err := ctx.Err(); err != nil {
		return results, fmt.Errorf("sync run interrupted: %w", err)
	}

	return results, nil
}

// failedResource converts a resource-level error into a SyncResult so the
// failure is counted and printed like any other, instead of vanishing from the
// totals. Resources that fail before their versions are known are reported
// against their configured version list.
func failedResource(res domain.Resource, err error) *domain.SyncResult {
	versions := res.Versions
	if len(versions) == 0 {
		versions = []string{""}
	}

	result := &domain.SyncResult{Resource: res}
	for _, version := range versions {
		result.Failed = append(result.Failed, domain.VersionResult{
			Version: version,
			Status:  domain.SyncStatusFailed,
			Error:   err,
			Message: err.Error(),
		})
		result.Operations = append(result.Operations, domain.OperationRecord{
			ResourceType: res.Type,
			Operation:    domain.OpFail,
			Source:       res.Source.String(),
			Destination:  res.Destination.String(),
			Version:      version,
			Message:      err.Error(),
		})
	}
	return result
}

// processResource handles the full sync lifecycle for a single resource:
// transporter lookup, version expansion, optional scanning, and sync.
func (e *Engine) processResource(ctx context.Context, res domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	rLogger := e.logger.With(
		slog.String("resource_type", res.Type.String()),
		slog.String("source", res.Source.String()),
		slog.String("destination", res.Destination.String()),
	)

	// Look up the appropriate transporter for this resource type.
	t, ok := e.transporters[res.Type]
	if !ok {
		return nil, fmt.Errorf("resource %s %s: no transporter for resource type %q: %w",
			res.Type, res.Source, res.Type, domain.ErrUnsupportedTransport)
	}

	// Expand version patterns into concrete version lists.
	expandedVersions, readOps, err := e.expandVersions(ctx, t, res, opts, rLogger)
	if err != nil {
		return nil, fmt.Errorf("resource %s %s: expand versions: %w", res.Type, res.Source, err)
	}

	rLogger.Debug("expanded versions",
		slog.Int("count", len(expandedVersions)),
		slog.Any("versions", expandedVersions),
	)

	// Build a modified resource with the expanded version list.
	expandedRes := res
	expandedRes.Versions = expandedVersions

	// Run scanner if configured.
	var scan scanOutcome
	if res.ScannerRef != "" {
		scan, err = e.scanVersions(ctx, expandedRes, rLogger)
		if err != nil {
			return nil, err
		}
		expandedRes.Versions = scan.passed
	}

	// Delegate the actual sync to the transporter.
	result, err := t.Sync(ctx, expandedRes, opts)
	if err != nil {
		return nil, fmt.Errorf("resource %s %s: sync: %w", res.Type, res.Source, err)
	}

	// Report scanner rejections alongside the sync outcome so they are counted
	// rather than quietly missing from the totals.
	result.Failed = append(result.Failed, scan.rejected...)

	// Prepend read and scan operations, in the order they happened.
	ops := make([]domain.OperationRecord, 0, len(readOps)+len(scan.ops)+len(result.Operations))
	ops = append(ops, readOps...)
	ops = append(ops, scan.ops...)
	ops = append(ops, result.Operations...)
	result.Operations = ops

	return result, nil
}

// expandVersions resolves version patterns (regex patterns) into concrete
// version strings by listing available versions and filtering with pattern
// matching. It also returns operation records for each ListVersions call.
func (e *Engine) expandVersions(ctx context.Context, t domain.Transporter, res domain.Resource, opts domain.SyncOptions, logger *slog.Logger) ([]string, []domain.OperationRecord, error) {
	var expanded []string
	var ops []domain.OperationRecord

	for _, version := range res.Versions {
		if !pattern.IsPattern(version) {
			// Literal version, use as-is.
			expanded = append(expanded, version)
			continue
		}

		// Version is a pattern; list available versions and filter.
		var creds *domain.Credential
		if res.SourceCredentialsRef != "" && opts.Credentials != nil {
			c, err := opts.Credentials.ResolveByRef(res.SourceCredentialsRef, credentialTypeForResource(res.Type))
			if err != nil {
				return nil, nil, fmt.Errorf("resolve source credentials for pattern expansion: %w", err)
			}
			creds = c
		}

		available, err := t.ListVersions(ctx, res.Source, creds)
		if err != nil {
			return nil, nil, fmt.Errorf("list versions for pattern %q: %w", version, err)
		}

		// Record the read operation.
		ops = append(ops, domain.OperationRecord{
			ResourceType: res.Type,
			Operation:    domain.OpRead,
			Source:       res.Source.String(),
			Destination:  res.Destination.String(),
			Message:      fmt.Sprintf("listed %d versions from source for pattern %q", len(available), version),
		})

		matched, err := pattern.Match(version, available)
		if err != nil {
			return nil, nil, fmt.Errorf("match pattern %q: %w", version, err)
		}

		logger.Debug("pattern matched",
			slog.String("pattern", version),
			slog.Int("count", len(matched)),
			slog.Any("matched", matched),
		)

		expanded = append(expanded, matched...)
	}

	return expanded, ops, nil
}

func credentialTypeForResource(resourceType domain.ResourceType) domain.CredentialType {
	switch resourceType {
	case domain.ResourceTypeImage:
		return domain.CredentialTypeImage
	case domain.ResourceTypeHelm:
		return domain.CredentialTypeHelm
	case domain.ResourceTypeGit:
		return domain.CredentialTypeGit
	default:
		return domain.CredentialType("")
	}
}

// scanOutcome separates the versions a scanner cleared from those it did not.
type scanOutcome struct {
	// passed lists the versions that may proceed to sync.
	passed []string

	// rejected reports every version held back, whether the scanner failed the
	// artifact or could not run at all. These are surfaced as failures: a
	// version that silently disappears from the totals is indistinguishable
	// from one that was never configured, and for an air-gapped mirror that
	// means a missing image nobody noticed until a pod fails to start.
	rejected []domain.VersionResult

	// ops records the rejections for the operation log.
	ops []domain.OperationRecord
}

// scanVersions runs the configured scanner against each version of the
// resource. If the scanner cannot be found, an error is returned.
func (e *Engine) scanVersions(ctx context.Context, res domain.Resource, logger *slog.Logger) (scanOutcome, error) {
	s, ok := e.scanners[res.ScannerRef]
	if !ok {
		return scanOutcome{}, fmt.Errorf("scanner %q not found", res.ScannerRef)
	}

	var out scanOutcome

	reject := func(version, message string, err error) {
		out.rejected = append(out.rejected, domain.VersionResult{
			Version: version,
			Status:  domain.SyncStatusFailed,
			Error:   err,
			Message: message,
		})
		out.ops = append(out.ops, domain.OperationRecord{
			ResourceType: res.Type,
			Operation:    domain.OpFail,
			Source:       res.Source.String(),
			Destination:  res.Destination.String(),
			Version:      version,
			Message:      message,
		})
	}

	for _, version := range res.Versions {
		artifact := domain.ArtifactRef{
			Type:       res.Type,
			Registry:   res.Source.Registry,
			Repository: res.Source.Repository,
			Version:    version,
		}

		result, err := s.Scan(ctx, artifact)
		if err != nil {
			logger.Error("scanner execution error",
				slog.String("scanner", res.ScannerRef),
				slog.String("version", version),
				slog.String("error", err.Error()),
			)
			reject(version, fmt.Sprintf("scanner %q could not run: %s", res.ScannerRef, err), err)
			continue
		}

		if !result.Passed {
			logger.Warn("scan failed, holding back version",
				slog.String("scanner", res.ScannerRef),
				slog.String("version", version),
				slog.Int("exit_code", result.ExitCode),
			)
			reject(version,
				fmt.Sprintf("scanner %q rejected the artifact (exit code %d)", res.ScannerRef, result.ExitCode),
				domain.ErrScanFailed)
			continue
		}

		logger.Debug("scan passed",
			slog.String("scanner", res.ScannerRef),
			slog.String("version", version),
		)
		out.passed = append(out.passed, version)
	}

	return out, nil
}
