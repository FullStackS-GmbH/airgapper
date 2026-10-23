package sync_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/sync"
)

func TestBuildReport(t *testing.T) {
	t.Parallel()

	results := []domain.SyncResult{{
		Resource: domain.Resource{
			Type:        domain.ResourceTypeGit,
			Source:      domain.Endpoint{Repository: "https://user:s3cret@host/org/repo.git"},
			Destination: domain.Endpoint{Repository: "https://dest/org/repo.git"},
		},
		Synced:  []domain.VersionResult{{Version: "main", Status: domain.SyncStatusSynced, Message: "pushed"}},
		Skipped: []domain.VersionResult{{Version: "v1", Status: domain.SyncStatusSkipped, Message: "already exists"}},
		Failed: []domain.VersionResult{{
			Version: "v2", Status: domain.SyncStatusFailed,
			Error: errors.New("clone https://user:s3cret@host/org/repo.git: refused"),
		}},
	}}

	report := sync.BuildReport(results)

	assert.Equal(t, sync.SummaryReport{Resources: 1, Versions: 3, Synced: 1, Skipped: 1, Failed: 1}, report.Summary)
	require.Len(t, report.Resources, 1)
	assert.Equal(t, "git", report.Resources[0].Type)
	assert.Equal(t, []string{"synced", "skipped", "failed"}, []string{
		report.Resources[0].Versions[0].Status, report.Resources[0].Versions[1].Status, report.Resources[0].Versions[2].Status,
	})

	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "s3cret", "URL credentials must be masked")
	assert.Contains(t, string(encoded), "https://***@host")
}

func TestBuildReport_EmptyRunEncodesEmptyLists(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(sync.BuildReport(nil))

	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"resources":[]`, "consumers should get a list, never null")
}
