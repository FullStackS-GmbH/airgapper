package scanner_test

import (
	"context"
	"testing"
	"time"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/config"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
	"github.com/fullstacks-gmbh/universal-airgapper/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandScanner_Name(t *testing.T) {
	t.Parallel()

	s := scanner.New("trivy", "trivy image {source}", 0, 30*time.Second)
	assert.Equal(t, "trivy", s.Name())
}

func TestCommandScanner_PlaceholderSubstitution(t *testing.T) {
	t.Parallel()

	// Use "echo" to verify the substituted command runs with correct args.
	s := scanner.New("echo-scanner", "echo {registry} {repository} {tag} {source} {type}", 0, 10*time.Second)

	artifact := domain.ArtifactRef{
		Type:       domain.ResourceTypeImage,
		Registry:   "registry.example.com",
		Repository: "repo/image",
		Version:    "v1.0.0",
	}

	result, err := s.Scan(context.Background(), artifact)
	require.NoError(t, err)
	require.True(t, result.Passed)

	assert.Contains(t, result.Output, "registry.example.com")
	assert.Contains(t, result.Output, "repo/image")
	assert.Contains(t, result.Output, "v1.0.0")
	assert.Contains(t, result.Output, "registry.example.com/repo/image:v1.0.0")
	assert.Contains(t, result.Output, "image")
}

func TestCommandScanner_PassingCommand(t *testing.T) {
	t.Parallel()

	s := scanner.New("pass", "true", 0, 10*time.Second)

	result, err := s.Scan(context.Background(), domain.ArtifactRef{})
	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Equal(t, 0, result.ExitCode)
}

func TestCommandScanner_FailingCommand(t *testing.T) {
	t.Parallel()

	s := scanner.New("fail", "false", 0, 10*time.Second)

	result, err := s.Scan(context.Background(), domain.ArtifactRef{})
	require.NoError(t, err)
	assert.False(t, result.Passed)
	assert.NotEqual(t, 0, result.ExitCode)
}

func TestCommandScanner_NonZeroSuccessCode(t *testing.T) {
	t.Parallel()

	// "false" returns exit code 1; if success_code is 1, it should pass.
	s := scanner.New("custom-exit", "false", 1, 10*time.Second)

	result, err := s.Scan(context.Background(), domain.ArtifactRef{})
	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Equal(t, 1, result.ExitCode)
}

func TestCommandScanner_BinaryNotFound(t *testing.T) {
	t.Parallel()

	s := scanner.New("missing", "nonexistent-binary-xyz123", 0, 10*time.Second)

	_, err := s.Scan(context.Background(), domain.ArtifactRef{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute command")
}

func TestCommandScanner_DefaultTimeout(t *testing.T) {
	t.Parallel()

	// timeout <= 0 should use the default (300s), not panic.
	s := scanner.New("default-timeout", "true", 0, 0)
	require.NotNil(t, s)

	result, err := s.Scan(context.Background(), domain.ArtifactRef{})
	require.NoError(t, err)
	assert.True(t, result.Passed)
}

func TestNewFromConfig(t *testing.T) {
	t.Parallel()

	configs := []config.ScannerConfig{
		{Name: "trivy", Command: "trivy image {source}", SuccessCode: 0, Timeout: 120},
		{Name: "grype", Command: "grype {source}", SuccessCode: 0, Timeout: 60},
	}

	scanners := scanner.NewFromConfig(configs)
	assert.Len(t, scanners, 2)

	_, ok := scanners["trivy"]
	assert.True(t, ok)

	_, ok = scanners["grype"]
	assert.True(t, ok)

	// Verify names match
	assert.Equal(t, "trivy", scanners["trivy"].Name())
	assert.Equal(t, "grype", scanners["grype"].Name())
}
