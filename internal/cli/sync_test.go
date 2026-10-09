package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncDryRunReportsDestinationFailureAndContinues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.git")
	_, err := gogit.PlainInit(empty, true)
	require.NoError(t, err)
	configPath := filepath.Join(dir, "git.airgapper.yaml")
	reportPath := filepath.Join(dir, "report.log")
	cfg := fmt.Sprintf(`resources:
  - type: git
    source_repo: https://example.com/source.git
    destination_repo: %s
    refs: [main, v1.0]
  - type: git
    source_repo: https://example.com/source.git
    destination_repo: %s
    refs: [main]
`, filepath.Join(dir, "missing.git"), empty)
	require.NoError(t, os.WriteFile(configPath, []byte(cfg), 0o600))
	cmd := NewRootCmd("test", "test", "test")
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--dry-run-log", reportPath})
	err = cmd.Execute()
	require.EqualError(t, err, "sync completed with 2 failures")
	assert.Contains(t, output.String(), "Resources: 2 | Versions: 3 | Synced: 0 | Skipped: 1 | Failed: 2")
	assert.Contains(t, output.String(), "check destination")
	assert.Contains(t, output.String(), "dry-run: would sync")
	report, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	assert.Contains(t, string(report), "FAILED")
	assert.Contains(t, string(report), "check destination")
	_, err = os.Stat(filepath.Join(dir, "missing.git"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create a destination")
}
