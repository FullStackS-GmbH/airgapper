package transport_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/transport"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSyncVersions_BucketsByStatus(t *testing.T) {
	t.Parallel()

	statuses := map[string]domain.SyncStatus{
		"a": domain.SyncStatusSynced,
		"b": domain.SyncStatusSkipped,
		"c": domain.SyncStatusFailed,
		"d": domain.SyncStatusSynced,
	}

	result := transport.SyncVersions([]string{"a", "b", "c", "d"}, func(version string) (domain.VersionResult, []domain.OperationRecord) {
		return domain.VersionResult{Version: version, Status: statuses[version]},
			[]domain.OperationRecord{{Version: version, Operation: domain.OpPush}}
	})

	assert.Equal(t, []string{"a", "d"}, versionsOf(result.Synced))
	assert.Equal(t, []string{"b"}, versionsOf(result.Skipped))
	assert.Equal(t, []string{"c"}, versionsOf(result.Failed))
	assert.Len(t, result.Operations, 4, "operations from every version are collected in order")
	assert.Equal(t, 4, result.TotalCount())
}

func TestSyncVersions_NoVersions(t *testing.T) {
	t.Parallel()

	result := transport.SyncVersions(nil, func(string) (domain.VersionResult, []domain.OperationRecord) {
		t.Fatal("syncOne must not be called when there are no versions")
		return domain.VersionResult{}, nil
	})

	require.NotNil(t, result)
	assert.Equal(t, 0, result.TotalCount())
	assert.False(t, result.HasFailures())
}

func TestDryRunResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pushMode domain.PushMode
		exists   bool
		wantOp   domain.OperationType
		wantMsg  string
	}{
		{"absent at destination", domain.PushModeSkip, false, domain.OpPush, "dry-run: would sync"},
		{"exists with skip", domain.PushModeSkip, true, domain.OpSkip, "dry-run: would skip (already exists)"},
		{"exists with force", domain.PushModeForce, true, domain.OpOverwrite, "dry-run: would overwrite (already exists)"},
		{"absent with force", domain.PushModeForce, false, domain.OpPush, "dry-run: would sync"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			op := func(operation domain.OperationType, msg string) domain.OperationRecord {
				return domain.OperationRecord{Operation: operation, Message: msg}
			}

			vr, ops := transport.DryRunResult(tt.pushMode, "v1.0.0", tt.exists, discardLogger(), op)

			// A dry run never writes, so the version is always reported skipped.
			assert.Equal(t, domain.SyncStatusSkipped, vr.Status)
			assert.Equal(t, "v1.0.0", vr.Version)
			assert.Equal(t, tt.wantMsg, vr.Message)
			require.Len(t, ops, 1)
			assert.Equal(t, tt.wantOp, ops[0].Operation)
			assert.Equal(t, tt.wantMsg, ops[0].Message)
		})
	}
}

func versionsOf(results []domain.VersionResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Version)
	}
	return out
}
