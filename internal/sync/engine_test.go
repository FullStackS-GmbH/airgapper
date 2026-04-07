package sync_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/sync"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/transport"
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

	factory := transport.NewFactory(mt)
	engine := sync.NewEngine(factory, nil, discardLogger())

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

	factory := transport.NewFactory(mt)
	engine := sync.NewEngine(factory, nil, discardLogger())

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

	factory := transport.NewFactory(mt)
	engine := sync.NewEngine(factory, nil, discardLogger())

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

	factory := transport.NewFactory(mt)
	scanners := map[string]domain.Scanner{"test-scanner": ms}
	engine := sync.NewEngine(factory, scanners, discardLogger())

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

	factory := transport.NewFactory(mt)
	engine := sync.NewEngine(factory, nil, discardLogger())

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

	factory := transport.NewFactory() // empty factory
	engine := sync.NewEngine(factory, nil, discardLogger())

	resources := []domain.Resource{
		{
			Type:     domain.ResourceTypeImage,
			Source:   domain.Endpoint{Registry: "docker.io", Repository: "library/ubuntu"},
			Versions: []string{"v1.0.0"},
		},
	}

	_, err := engine.Run(context.Background(), resources, domain.SyncOptions{
		Logger: discardLogger(),
	})
	require.Error(t, err)
}
