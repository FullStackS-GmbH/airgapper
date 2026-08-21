package sync_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
)

// mockTransporter implements domain.Transporter for testing.
type mockTransporter struct {
	typeFn         func() domain.ResourceType
	syncFn         func(ctx context.Context, r domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error)
	existsFn       func(ctx context.Context, ep domain.Endpoint, version string, creds *domain.Credential) (bool, error)
	listVersionsFn func(ctx context.Context, ep domain.Endpoint, creds *domain.Credential) ([]string, error)
}

func (m *mockTransporter) Type() domain.ResourceType {
	return m.typeFn()
}

func (m *mockTransporter) Sync(ctx context.Context, r domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
	return m.syncFn(ctx, r, opts)
}

func (m *mockTransporter) Exists(ctx context.Context, ep domain.Endpoint, version string, creds *domain.Credential) (bool, error) {
	return m.existsFn(ctx, ep, version, creds)
}

func (m *mockTransporter) ListVersions(ctx context.Context, ep domain.Endpoint, creds *domain.Credential) ([]string, error) {
	return m.listVersionsFn(ctx, ep, creds)
}

// mockScanner implements domain.Scanner for testing.
type mockScanner struct {
	name   string
	scanFn func(ctx context.Context, artifact domain.ArtifactRef) (*domain.ScanResult, error)
}

func (m *mockScanner) Name() string { return m.name }

func (m *mockScanner) Scan(ctx context.Context, artifact domain.ArtifactRef) (*domain.ScanResult, error) {
	return m.scanFn(ctx, artifact)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestEngine_SingleResource(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			return &domain.SyncResult{
				Resource: r,
				Synced:   []domain.VersionResult{{Version: "v1.0.0", Status: domain.SyncStatusSynced}},
			}, nil
		},
		listVersionsFn: func(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	transporters := []domain.Transporter{mt}
	engine := sync.NewEngine(transporters, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: "library/ubuntu"},
			Versions:    []string{"v1.0.0"},
			PushMode:    domain.PushModeSkip,
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Len(t, results[0].Synced, 1)
	assert.Equal(t, "v1.0.0", results[0].Synced[0].Version)
}

func TestEngine_PatternExpansion(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			var synced []domain.VersionResult
			for _, v := range r.Versions {
				synced = append(synced, domain.VersionResult{Version: v, Status: domain.SyncStatusSynced})
			}
			return &domain.SyncResult{Resource: r, Synced: synced}, nil
		},
		listVersionsFn: func(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
			return []string{"v1.0.0", "v1.1.0", "v2.0.0", "v2.1.0"}, nil
		},
		existsFn: func(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	transporters := []domain.Transporter{mt}
	engine := sync.NewEngine(transporters, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: "library/ubuntu"},
			Versions:    []string{"v1\\..*"}, // pattern: matches v1.0.0 and v1.1.0
			PushMode:    domain.PushModeSkip,
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Len(t, results[0].Synced, 2)
}

func TestEngine_DryRun(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, opts domain.SyncOptions) (*domain.SyncResult, error) {
			var skipped []domain.VersionResult
			for _, v := range r.Versions {
				skipped = append(skipped, domain.VersionResult{
					Version: v,
					Status:  domain.SyncStatusSkipped,
					Message: "dry-run",
				})
			}
			return &domain.SyncResult{Resource: r, Skipped: skipped}, nil
		},
		listVersionsFn: func(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	transporters := []domain.Transporter{mt}
	engine := sync.NewEngine(transporters, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: "library/ubuntu"},
			Versions:    []string{"v1.0.0"},
			PushMode:    domain.PushModeSkip,
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		DryRun: true,
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Len(t, results[0].Skipped, 1)
	assert.Empty(t, results[0].Synced)
}

func TestEngine_ScannerFailureFiltering(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			var synced []domain.VersionResult
			for _, v := range r.Versions {
				synced = append(synced, domain.VersionResult{Version: v, Status: domain.SyncStatusSynced})
			}
			return &domain.SyncResult{Resource: r, Synced: synced}, nil
		},
		listVersionsFn: func(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	ms := &mockScanner{
		name: "test-scanner",
		scanFn: func(_ context.Context, artifact domain.ArtifactRef) (*domain.ScanResult, error) {
			// Only v1.0.0 passes the scan
			if artifact.Version == "v1.0.0" {
				return &domain.ScanResult{Passed: true, ExitCode: 0}, nil
			}
			return &domain.ScanResult{Passed: false, ExitCode: 1}, nil
		},
	}

	transporters := []domain.Transporter{mt}
	scanners := map[string]domain.Scanner{"test-scanner": ms}
	engine := sync.NewEngine(transporters, scanners, discardLogger())

	resources := []domain.Resource{
		{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: "library/ubuntu"},
			Versions:    []string{"v1.0.0", "v2.0.0"},
			PushMode:    domain.PushModeSkip,
			ScannerRef:  "test-scanner",
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	// Only v1.0.0 should have been synced (v2.0.0 filtered by scanner)
	assert.Len(t, results[0].Synced, 1)
	assert.Equal(t, "v1.0.0", results[0].Synced[0].Version)

	// The rejected version must be reported as failed, not dropped: a version
	// missing from the totals is indistinguishable from one never configured.
	require.Len(t, results[0].Failed, 1)
	assert.Equal(t, "v2.0.0", results[0].Failed[0].Version)
	require.ErrorIs(t, results[0].Failed[0].Error, domain.ErrScanFailed)
	assert.Equal(t, 2, results[0].TotalCount())
}

func TestEngine_ScannerExecutionErrorIsReportedNotDropped(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			var synced []domain.VersionResult
			for _, v := range r.Versions {
				synced = append(synced, domain.VersionResult{Version: v, Status: domain.SyncStatusSynced})
			}
			return &domain.SyncResult{Resource: r, Synced: synced}, nil
		},
		listVersionsFn: func(context.Context, domain.Endpoint, *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(context.Context, domain.Endpoint, string, *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	// The scanner binary cannot be executed at all — distinct from a scan that
	// runs and fails the artifact.
	ms := &mockScanner{
		name: "broken-scanner",
		scanFn: func(context.Context, domain.ArtifactRef) (*domain.ScanResult, error) {
			return nil, fmt.Errorf("exec: %q: executable file not found in $PATH", "trivy")
		},
	}

	engine := sync.NewEngine(
		[]domain.Transporter{mt},
		map[string]domain.Scanner{"broken-scanner": ms},
		discardLogger(),
	)

	resources := []domain.Resource{
		{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: "library/ubuntu"},
			Versions:    []string{"v1.0.0", "v2.0.0"},
			ScannerRef:  "broken-scanner",
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Empty(t, results[0].Synced, "nothing may be promoted when the scanner never ran")
	assert.Len(t, results[0].Failed, 2, "both versions must be reported, not silently skipped")
	assert.Equal(t, 2, results[0].TotalCount())
	assert.Contains(t, results[0].Failed[0].Message, "could not run")
}

func TestEngine_MultipleResourcesConcurrent(t *testing.T) {
	t.Parallel()

	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			var synced []domain.VersionResult
			for _, v := range r.Versions {
				synced = append(synced, domain.VersionResult{Version: v, Status: domain.SyncStatusSynced})
			}
			return &domain.SyncResult{Resource: r, Synced: synced}, nil
		},
		listVersionsFn: func(_ context.Context, _ domain.Endpoint, _ *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(_ context.Context, _ domain.Endpoint, _ string, _ *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	transporters := []domain.Transporter{mt}
	engine := sync.NewEngine(transporters, nil, discardLogger())

	resources := make([]domain.Resource, 10)
	for i := range resources {
		resources[i] = domain.Resource{
			Type:        domain.ResourceTypeImage,
			Source:      domain.Endpoint{Registry: "docker.io", Repository: fmt.Sprintf("org/app%d", i)},
			Destination: domain.Endpoint{Registry: "internal.io", Repository: fmt.Sprintf("org/app%d", i)},
			Versions:    []string{fmt.Sprintf("v%d.0.0", i)},
			PushMode:    domain.PushModeSkip,
		}
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	assert.Len(t, results, 10)

	// Each result should have 1 synced version
	for _, r := range results {
		assert.Len(t, r.Synced, 1)
	}
}

func TestEngine_UnsupportedTransport(t *testing.T) {
	t.Parallel()

	engine := sync.NewEngine(nil, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:     domain.ResourceTypeImage,
			Source:   domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Versions: []string{"v1.0.0"},
		},
	}

	// A resource with no registered transporter is a failed resource, not a
	// failed run: the report must name it rather than the run aborting.
	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	require.Len(t, results[0].Failed, 1)
	assert.Equal(t, "v1.0.0", results[0].Failed[0].Version)
	require.ErrorIs(t, results[0].Failed[0].Error, domain.ErrUnsupportedTransport)
	assert.True(t, results[0].HasFailures())
}

func TestEngine_OneFailingResourceDoesNotAbortTheRest(t *testing.T) {
	t.Parallel()

	// Only the image transporter is registered, so the helm resource fails.
	// The image resource must still be synced and reported.
	mt := &mockTransporter{
		typeFn: func() domain.ResourceType { return domain.ResourceTypeImage },
		syncFn: func(_ context.Context, r domain.Resource, _ domain.SyncOptions) (*domain.SyncResult, error) {
			return &domain.SyncResult{
				Resource: r,
				Synced:   []domain.VersionResult{{Version: "v1.0.0", Status: domain.SyncStatusSynced}},
			}, nil
		},
		listVersionsFn: func(context.Context, domain.Endpoint, *domain.Credential) ([]string, error) {
			return nil, nil
		},
		existsFn: func(context.Context, domain.Endpoint, string, *domain.Credential) (bool, error) {
			return false, nil
		},
	}

	engine := sync.NewEngine([]domain.Transporter{mt}, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:     domain.ResourceTypeHelm,
			Source:   domain.Endpoint{Registry: "charts.example.com", Repository: "nginx"},
			Versions: []string{"1.0.0", "2.0.0"},
		},
		{
			Type:     domain.ResourceTypeImage,
			Source:   domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Versions: []string{"v1.0.0"},
		},
	}

	results, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.NoError(t, err)
	require.Len(t, results, 2, "both resources must be reported")

	summary := sync.Summarize(results)
	assert.Equal(t, 1, summary.Synced, "the healthy resource still syncs")
	assert.Equal(t, 2, summary.Failed, "both versions of the broken resource are counted")
	assert.Equal(t, 3, summary.TotalVersions, "no version silently disappears")
}
