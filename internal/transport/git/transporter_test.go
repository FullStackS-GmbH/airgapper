package git

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestGitDryRunDestination(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		status        int
		refs          map[string]plumbing.Hash
		wantFailure   bool
		wantMessage   string
		wantOperation domain.OperationType
	}{
		{name: "missing", status: http.StatusNotFound, wantFailure: true},
		{name: "unauthenticated", status: http.StatusUnauthorized, wantFailure: true},
		{name: "forbidden", status: http.StatusForbidden, wantFailure: true},
		{name: "server failure", status: http.StatusInternalServerError, wantFailure: true},
		{name: "empty repository", status: http.StatusOK, wantMessage: "dry-run: would sync", wantOperation: domain.OpPush},
		{name: "absent ref", status: http.StatusOK, refs: map[string]plumbing.Hash{"refs/heads/other": plumbing.NewHash("0123456789012345678901234567890123456789")}, wantMessage: "dry-run: would sync", wantOperation: domain.OpPush},
		{name: "existing ref", status: http.StatusOK, refs: map[string]plumbing.Hash{"refs/heads/main": plumbing.NewHash("0123456789012345678901234567890123456789")}, wantMessage: "dry-run: would skip (already exists)", wantOperation: domain.OpSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var advertisement bytes.Buffer
			advertisement.WriteString("001e# service=git-upload-pack\n0000")
			refs := packp.NewAdvRefs()
			if tc.refs != nil {
				refs.References = tc.refs
			}
			require.NoError(t, refs.Encode(&advertisement))
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				w.WriteHeader(tc.status)
				_, _ = w.Write(advertisement.Bytes())
			}))
			defer server.Close()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			transporter := New(logger)
			resource := domain.Resource{
				Type:        domain.ResourceTypeGit,
				Source:      domain.Endpoint{Repository: server.URL + "/source.git"},
				Destination: domain.Endpoint{Repository: server.URL + "/target.git"},
				Versions:    []string{"main"}, PushMode: domain.PushModeSkip,
			}
			result, err := transporter.Sync(context.Background(), resource, domain.SyncOptions{DryRun: true})
			require.NoError(t, err)
			require.Len(t, result.Operations, 1)
			assert.Zero(t, writes.Load(), "dry-run must never push")
			if tc.wantFailure {
				require.Len(t, result.Failed, 1)
				assert.Empty(t, result.Skipped)
				assert.Error(t, result.Failed[0].Error)
				assert.Contains(t, result.Failed[0].Message, "check destination")
				assert.Equal(t, domain.OpFail, result.Operations[0].Operation)
			} else {
				assert.Empty(t, result.Failed)
				require.Len(t, result.Skipped, 1)
				assert.Equal(t, tc.wantMessage, result.Skipped[0].Message)
				assert.Equal(t, tc.wantOperation, result.Operations[0].Operation)
			}
		})
	}
}

func TestExistsPreservesConnectionAndCancellationErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	transporter := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	exists, err := transporter.Exists(context.Background(), domain.Endpoint{Repository: server.URL + "/repo.git"}, "main", nil)
	assert.False(t, exists)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exists, err = transporter.Exists(ctx, domain.Endpoint{Repository: server.URL + "/repo.git"}, "main", nil)
	assert.False(t, exists)
	require.ErrorIs(t, err, context.Canceled)
}
