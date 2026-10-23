package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCLI executes the root command and returns stdout and the error.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd("test", "test", "test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const validConfig = `scanners:
  - name: trivy
    command: trivy image {source}
resources:
  - type: image
    source: a.io/x
    destination: b.io/x
    scanner_ref: trivy
    target_credentials_ref: reg
    tags: [v1]
`

const validCreds = `image:
  - name: reg
    username: u
    password: p
`

func TestValidate_ValidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := writeFile(t, dir, "ok.config.airgapper.yaml", validConfig)
	creds := writeFile(t, dir, "ok.creds.airgapper.yaml", validCreds)

	out, err := runCLI(t, "validate", "--config", cfg, "--credentials", creds)

	require.NoError(t, err)
	assert.Equal(t, "Configuration OK: 1 resources, 1 scanners\n", out)
}

func TestValidate_ListsEveryProblemAndExits2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := writeFile(t, dir, "bad.config.airgapper.yaml", `resources:
  - type: image
    source: a.io/x
    destination: b.io/x
    target_credentials_ref: missing
    policy_path: /does/not/exist.json
    tags: [v1]
  - type: helm
    source_registry: a.io
    source_chart: c
    destination_registry: b.io
    destination_repo: r
    versions: ["1"]
    policy_path: p.json
`)
	creds := writeFile(t, dir, "c.creds.airgapper.yaml", validCreds)

	out, err := runCLI(t, "validate", "--config", cfg, "--credentials", creds)

	require.Error(t, err)
	assert.Equal(t, 2, ExitCode(err))
	for _, want := range []string{"resource[1]", "policy_path applies to image resources only"} {
		assert.Contains(t, out, want)
	}
}

func TestValidate_ReferenceWithoutCredentialsFile(t *testing.T) {
	t.Parallel()
	cfg := writeFile(t, t.TempDir(), "c.config.airgapper.yaml", validConfig)

	out, err := runCLI(t, "validate", "--config", cfg)

	assert.Equal(t, 2, ExitCode(err))
	assert.Contains(t, out, "--credentials")
}

func TestValidate_JSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := writeFile(t, dir, "ok.config.airgapper.yaml", "resources: []\n")
	bad := writeFile(t, dir, "bad.config.airgapper.yaml", "resources:\n  - type: bogus\n")

	for _, tc := range []struct {
		name      string
		path      string
		wantValid bool
		wantExit  int
	}{
		{"valid", good, true, 0},
		{"invalid", bad, false, 2},
	} {
		out, err := runCLI(t, "validate", "--config", tc.path, "--output", "json")

		assert.Equal(t, tc.wantExit, ExitCode(err), tc.name)
		var report validateReport
		require.NoError(t, json.Unmarshal([]byte(out), &report), tc.name+": stdout must be exactly one JSON document")
		assert.Equal(t, tc.wantValid, report.Valid, tc.name)
		assert.NotNil(t, report.Problems, "problems is a list, never null")
		assert.Equal(t, !tc.wantValid, len(report.Problems) > 0, tc.name)
	}
}

func TestValidate_RejectsUnknownOutputFormat(t *testing.T) {
	t.Parallel()
	cfg := writeFile(t, t.TempDir(), "c.config.airgapper.yaml", "resources: []\n")

	_, err := runCLI(t, "validate", "--config", cfg, "--output", "xml")

	assert.Equal(t, 2, ExitCode(err))
}
