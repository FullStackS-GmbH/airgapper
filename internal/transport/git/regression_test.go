package git

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func TestRegressionGitListVersionsHonorsCancellation(t *testing.T) {
	t.Parallel()
	var advertisement bytes.Buffer
	advertisement.WriteString("001e# service=git-upload-pack\n0000")
	refs := packp.NewAdvRefs()
	refs.References = map[string]plumbing.Hash{"refs/heads/main": plumbing.NewHash("0123456789012345678901234567890123456789")}
	require.NoError(t, refs.Encode(&advertisement))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(advertisement.Bytes())
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transporter := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := transporter.ListVersions(ctx, domain.Endpoint{Repository: server.URL + "/probe.git"}, nil)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, requests.Load(), "a canceled operation must not list remote refs")
}

func TestRegressionGitListVersionsCancelsInFlightRequest(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-release:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	// Release the fixture even when the client ignores cancellation.
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(slog.New(slog.NewTextHandler(io.Discard, nil))).ListVersions(ctx,
			domain.Endpoint{Repository: server.URL + "/probe.git"}, nil)
		done <- err
	}()
	returned := false
	t.Cleanup(func() {
		if !returned {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("Git request did not finish after fixture release")
			}
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Git request never reached the fixture")
	}
	cancel()
	select {
	case err := <-done:
		returned = true
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Error("Git version listing continued after cancellation")
	}
}
