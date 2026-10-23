package helm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

func ociPullResource(server *httptest.Server) domain.Resource {
	return domain.Resource{
		Type:   domain.ResourceTypeHelm,
		Source: domain.Endpoint{Registry: "oci://" + strings.TrimPrefix(server.URL, "http://"), Repository: "probe"},
	}
}

func TestPullChartBytesStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done() // hold the request open until the client gives up
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(slog.New(slog.NewTextHandler(io.Discard, nil))).PullChartBytes(ctx, ociPullResource(server), "1.0.0", nil)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the server")
	}
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("the pull kept running after cancellation")
	}
}

func TestPullChartBytesWithCancelledContextSendsNothing(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(slog.New(slog.NewTextHandler(io.Discard, nil))).PullChartBytes(ctx, ociPullResource(server), "1.0.0", nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, requests.Load())
}
