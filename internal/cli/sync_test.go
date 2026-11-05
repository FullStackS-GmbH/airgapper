package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/sync"
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

func TestSyncJSONOutput(t *testing.T) {
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
    refs: [refs/heads/main]
`, empty)
	require.NoError(t, os.WriteFile(configPath, []byte(cfg), 0o600))

	out, err := runCLI(t, "sync", "--config", configPath, "--dry-run", "--dry-run-log", reportPath, "--output", "json")

	require.NoError(t, err)
	var report sync.Report
	require.NoError(t, json.Unmarshal([]byte(out), &report), "stdout must be exactly one JSON document")
	assert.Equal(t, reportPath, report.DryRunLog)
	assert.Equal(t, 1, report.Summary.Skipped)
	require.Len(t, report.Resources, 1)
	assert.Equal(t, "skipped", report.Resources[0].Versions[0].Status)
}

func TestSyncGenericFileCLI(t *testing.T) {
	t.Parallel()
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			w.WriteHeader(404)
		case "GET":
			_, _ = io.WriteString(w, "test")
		case "PUT":
			puts++
			user, password, _ := r.BasicAuth()
			if user != "svc" || password != "dummy" {
				t.Error("file credential not loaded")
			}
			w.WriteHeader(201)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "file.airgapper.yaml")
	creds := filepath.Join(dir, "file.creds.airgapper.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(fmt.Sprintf("resources:\n  - type: generic\n    source: %s/source\n    destination: %s/destination\n    target_credentials_ref: test\n", server.URL, server.URL)), 0600))
	require.NoError(t, os.WriteFile(creds, []byte("file:\n  - name: test\n    username: svc\n    password: dummy\n"), 0600))
	out, err := runCLI(t, "sync", "--config", cfg, "--credentials", creds, "--output", "json")
	require.NoError(t, err)
	var report sync.Report
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.Equal(t, 1, report.Summary.Synced)
	require.Equal(t, 1, puts)
}
