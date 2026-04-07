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
	"github.com/fullstacks-gmbh/airgapper/internal/logging"
	"github.com/fullstacks-gmbh/airgapper/internal/pattern"
	"github.com/fullstacks-gmbh/airgapper/internal/transport"
)

// maxConcurrency is the maximum number of resources processed in parallel.
const maxConcurrency = 4

// Engine orchestrates the sync workflow across all resources. It resolves
// transporters, expands version patterns, runs optional scanners, and
// delegates the actual copy to the appropriate transporter.
type Engine struct {
	// factory provides transporter lookup by resource type.
	factory *transport.Factory

	// scanners maps scanner names to their implementations.
	scanners map[string]domain.Scanner

	// logger is the structured logger for the engine.
	logger *slog.Logger
}

// NewEngine creates a sync engine with the given transport factory, scanner
// map, and logger.
func NewEngine(factory *transport.Factory, scanners map[string]domain.Scanner, logger *slog.Logger) *Engine {
	return &Engine{
		factory:  factory,
		scanners: scanners,
		logger:   logger,
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
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for _, res := range resources {
		res := res // capture loop variable
		g.Go(func() error {
			result, err := e.processResource(gctx, res, opts)
			if err != nil {
				return err
			}

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return results, err
	}

	return results, nil
}

// processResource handles the full sync lifecycle for a single resource:
// transporter lookup, version expansion, optional scanning, and sync.
func (e *Engine) processResource(ctx context.Context, res domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	rLogger := logging.WithResource(e.logger, res.Type.String(), res.Source.String(), res.Destination.String())

	// Look up the appropriate transporter for this resource type.
	t, err := e.factory.ForType(res.Type)
	if err != nil {
		return nil, fmt.Errorf("resource %s %s: %w", res.Type, res.Source, err)
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
	if res.ScannerRef != "" {
		expandedRes, err = e.scanVersions(ctx, expandedRes, rLogger)
		if err != nil {
			return nil, err
		}
	}

	// Delegate the actual sync to the transporter.
	result, err := t.Sync(ctx, expandedRes, opts)
	if err != nil {
		return nil, fmt.Errorf("resource %s %s: sync: %w", res.Type, res.Source, err)
	}

	// Prepend read operations from version expansion.
	result.Operations = append(readOps, result.Operations...)

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
			c, err := opts.Credentials.ResolveByRef(res.SourceCredentialsRef)
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

// scanVersions runs the configured scanner against each version of the
// resource. Versions that fail the scan are removed from the resource's
// version list and the method returns the modified resource. If the scanner
// cannot be found, an error is returned.
func (e *Engine) scanVersions(ctx context.Context, res domain.Resource, logger *slog.Logger) (domain.Resource, error) {
	s, ok := e.scanners[res.ScannerRef]
	if !ok {
		return res, fmt.Errorf("scanner %q not found", res.ScannerRef)
	}

	var passedVersions []string
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
			continue
		}

		if result.Passed {
			logger.Debug("scan passed",
				slog.String("scanner", res.ScannerRef),
				slog.String("version", version),
			)
			passedVersions = append(passedVersions, version)
		} else {
			logger.Warn("scan failed, skipping version",
				slog.String("scanner", res.ScannerRef),
				slog.String("version", version),
				slog.Int("exit_code", result.ExitCode),
			)
		}
	}

	res.Versions = passedVersions
	return res, nil
}
