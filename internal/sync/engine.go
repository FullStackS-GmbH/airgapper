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

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/logging"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/pattern"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/transport"
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
	expandedVersions, err := e.expandVersions(ctx, t, res, opts)
	if err != nil {
		return nil, fmt.Errorf("resource %s %s: expand versions: %w", res.Type, res.Source, err)
	}

	rLogger.Debug("expanded versions", slog.Int("count", len(expandedVersions)))

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

	return result, nil
}

// expandVersions resolves version patterns (regex patterns) into concrete
// version strings by listing available versions and filtering with pattern
// matching.
func (e *Engine) expandVersions(ctx context.Context, t domain.Transporter, res domain.Resource, opts domain.SyncOptions) ([]string, error) {
	var expanded []string

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
				return nil, fmt.Errorf("resolve source credentials for pattern expansion: %w", err)
			}
			creds = c
		}

		available, err := t.ListVersions(ctx, res.Source, creds)
		if err != nil {
			return nil, fmt.Errorf("list versions for pattern %q: %w", version, err)
		}

		matched, err := pattern.Match(version, available)
		if err != nil {
			return nil, fmt.Errorf("match pattern %q: %w", version, err)
		}

		expanded = append(expanded, matched...)
	}

	return expanded, nil
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
