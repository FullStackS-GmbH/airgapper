package git

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// runGit runs the git CLI in dir and returns trimmed stdout. Fixtures use the
// real git binary so the transporter is exercised against genuine repositories
// over the file transport, without a network.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// newSourceRepo creates a repository with branch main, branch feature and tag v1.
func newSourceRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "tag", "v1")
	return dir
}

func newBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "--bare")
	return dir
}

func syncResource(src, dst string, mode domain.PushMode, refs ...string) domain.Resource {
	return domain.Resource{
		Type:        domain.ResourceTypeGit,
		Source:      domain.Endpoint{Repository: "file://" + src},
		Destination: domain.Endpoint{Repository: "file://" + dst},
		Versions:    refs,
		PushMode:    mode,
	}
}

func runSync(t *testing.T, res domain.Resource) *domain.SyncResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := New(logger).Sync(context.Background(), res, domain.SyncOptions{Logger: logger})
	require.NoError(t, err)
	return result
}

func TestSyncCopiesShortAndFullRefNames(t *testing.T) {
	t.Parallel()
	src, dst := newSourceRepo(t), newBareRepo(t)

	result := runSync(t, syncResource(src, dst, domain.PushModeSkip, "main", "v1", "refs/heads/feature"))

	require.Empty(t, result.Failed)
	assert.Len(t, result.Synced, 3)
	refs := runGit(t, dst, "for-each-ref", "--format=%(refname)")
	assert.ElementsMatch(t, []string{"refs/heads/main", "refs/heads/feature", "refs/tags/v1"}, strings.Fields(refs))
}

func TestSyncUnknownRefFailsOnlyThatRef(t *testing.T) {
	t.Parallel()
	src, dst := newSourceRepo(t), newBareRepo(t)

	result := runSync(t, syncResource(src, dst, domain.PushModeSkip, "nope", "main"))

	require.Len(t, result.Failed, 1)
	assert.ErrorIs(t, result.Failed[0].Error, domain.ErrNotFound)
	assert.Len(t, result.Synced, 1)
}

func TestSyncSkipKeepsExistingRefAndForceUpdatesIt(t *testing.T) {
	t.Parallel()
	src, dst := newSourceRepo(t), newBareRepo(t)
	require.Len(t, runSync(t, syncResource(src, dst, domain.PushModeSkip, "main")).Synced, 1)
	before := runGit(t, dst, "rev-parse", "refs/heads/main")

	runGit(t, src, "commit", "-q", "--allow-empty", "-m", "second")
	want := runGit(t, src, "rev-parse", "refs/heads/main")
	require.NotEqual(t, before, want)

	skipped := runSync(t, syncResource(src, dst, domain.PushModeSkip, "main"))
	assert.Len(t, skipped.Skipped, 1)
	assert.Equal(t, before, runGit(t, dst, "rev-parse", "refs/heads/main"), "skip must not touch an existing ref")

	forced := runSync(t, syncResource(src, dst, domain.PushModeForce, "main"))
	assert.Len(t, forced.Synced, 1)
	assert.Equal(t, want, runGit(t, dst, "rev-parse", "refs/heads/main"))
}

func TestSyncForceOnUnchangedRefSucceeds(t *testing.T) {
	t.Parallel()
	src, dst := newSourceRepo(t), newBareRepo(t)
	res := syncResource(src, dst, domain.PushModeForce, "main")
	require.Len(t, runSync(t, res).Synced, 1)

	again := runSync(t, res)

	assert.Empty(t, again.Failed, "an already up-to-date destination is not a failure")
	assert.Len(t, again.Synced, 1)
}

func TestListVersionsReturnsShortNames(t *testing.T) {
	t.Parallel()
	src := newSourceRepo(t)

	got, err := New(slog.New(slog.NewTextHandler(io.Discard, nil))).
		ListVersions(context.Background(), domain.Endpoint{Repository: "file://" + src}, nil)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"main", "feature", "v1"}, got)
}
