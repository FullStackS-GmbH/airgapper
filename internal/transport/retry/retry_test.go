package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker"
)

type statusErr int

func (e statusErr) Error() string   { return fmt.Sprintf("status %d", int(e)) }
func (e statusErr) HTTPStatus() int { return int(e) }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestIsTransient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"503 from our own type", statusErr(503), true},
		{"429", statusErr(429), true},
		{"408", statusErr(408), true},
		{"400", statusErr(400), false},
		{"401", statusErr(401), false},
		{"404", statusErr(404), false},
		{"registry rate limit", fmt.Errorf("wrapped: %w", docker.ErrTooManyRequests), true},
		{"registry status 502", fmt.Errorf("x: %w", docker.UnexpectedHTTPStatusError{StatusCode: 502}), true},
		{"registry status 403", fmt.Errorf("x: %w", docker.UnexpectedHTTPStatusError{StatusCode: 403}), false},
		{"registry text 503", errors.New(`reading digest v1 in r/app: StatusCode: 503, ""`), true},
		{"registry text 404", errors.New(`reading digest v1 in r/app: StatusCode: 404, ""`), false},
		{"timeout", fmt.Errorf("x: %w", timeoutErr{}), true},
		{"connection reset", fmt.Errorf("x: %w", syscall.ECONNRESET), true},
		{"connection refused", fmt.Errorf("x: %w", syscall.ECONNREFUSED), true},
		{"truncated body", fmt.Errorf("x: %w", io.ErrUnexpectedEOF), true},
		{"cancelled", context.Canceled, false},
		{"unknown", errors.New("boom"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsTransient(tc.err))
		})
	}
}

func TestDo(t *testing.T) {
	t.Parallel()
	fast := Policy{Attempts: 3, BaseDelay: time.Millisecond}

	t.Run("succeeds after transient failures", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := fast.Do(context.Background(), nil, "op", func() error {
			calls++
			if calls < 3 {
				return statusErr(503)
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("gives up after the configured attempts", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := fast.Do(context.Background(), nil, "op", func() error { calls++; return statusErr(503) })
		require.ErrorIs(t, err, statusErr(503))
		assert.Equal(t, 3, calls)
	})

	t.Run("does not retry a terminal error", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := fast.Do(context.Background(), nil, "op", func() error { calls++; return statusErr(404) })
		require.Error(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("zero policy tries once", func(t *testing.T) {
		t.Parallel()
		calls := 0
		_ = Policy{}.Do(context.Background(), nil, "op", func() error { calls++; return statusErr(503) })
		assert.Equal(t, 1, calls)
	})

	t.Run("stops waiting when the context ends", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		slow := Policy{Attempts: 5, BaseDelay: time.Minute}
		start := time.Now()
		err := slow.Do(ctx, nil, "op", func() error { cancel(); return statusErr(503) })
		require.ErrorIs(t, err, statusErr(503))
		assert.Less(t, time.Since(start), 5*time.Second)
	})
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	t.Parallel()
	p := Policy{Attempts: 20, BaseDelay: time.Second}
	for attempt := 1; attempt <= 15; attempt++ {
		d := p.backoff(attempt)
		assert.LessOrEqual(t, d, maxDelay)
		assert.GreaterOrEqual(t, d, min(p.BaseDelay<<min(attempt-1, 10), maxDelay)/2)
	}
}
