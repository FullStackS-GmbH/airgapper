// Package retry runs a network operation again after a transient failure.
package retry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"go.podman.io/image/v5/docker"
)

// maxDelay caps the wait between two tries.
const maxDelay = 30 * time.Second

// Policy says how often and how patiently to retry. The zero value tries once.
type Policy struct {
	// Attempts is the total number of tries including the first; values below
	// 2 mean no retry.
	Attempts int

	// BaseDelay is the wait before the first retry. It doubles for each
	// further retry, up to maxDelay.
	BaseDelay time.Duration
}

// Do runs op and, while it fails with a transient error, runs it again after a
// backoff wait. It gives up after p.Attempts tries, on a terminal error, or as
// soon as ctx ends, and returns the last error. what names the operation in the
// log line written for each retry; logger may be nil.
func (p Policy) Do(ctx context.Context, logger *slog.Logger, what string, op func() error) error {
	for attempt := 1; ; attempt++ {
		err := op()
		if err == nil || attempt >= p.Attempts || ctx.Err() != nil || !IsTransient(err) {
			return err
		}

		delay := p.backoff(attempt)
		if logger != nil {
			logger.Warn("transient failure, retrying",
				"operation", what, "attempt", attempt, "delay", delay, "error", err)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

// backoff doubles the delay with each attempt and adds jitter (half fixed, half
// random) so resources that fail together do not retry in lockstep.
func (p Policy) backoff(attempt int) time.Duration {
	d := min(p.BaseDelay<<min(attempt-1, 10), maxDelay)
	return d/2 + rand.N(d/2+1)
}

// statusText finds the code in the message of the registry library's
// unexported error type, e.g. "StatusCode: 503, ...".
var statusText = regexp.MustCompile(`StatusCode: (\d{3})`)

// IsTransient reports whether err is worth retrying: HTTP 408, 429 and 5xx,
// timeouts, reset or refused connections and truncated responses. Credential
// errors, missing artifacts and a cancelled context are terminal.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, docker.ErrTooManyRequests) {
		return true
	}
	if code, ok := httpStatus(err); ok {
		return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, target := range []error{io.ErrUnexpectedEOF, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED, syscall.EPIPE} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// httpStatus extracts an HTTP status code from the error types the transports
// produce.
func httpStatus(err error) (int, bool) {
	// Our own legacy chart repository error.
	var withStatus interface{ HTTPStatus() int }
	if errors.As(err, &withStatus) {
		return withStatus.HTTPStatus(), true
	}

	var registryErr docker.UnexpectedHTTPStatusError
	if errors.As(err, &registryErr) {
		return registryErr.StatusCode, true
	}

	// go-git's UnexpectedError has no Unwrap, so look inside it by hand.
	var gitErr *plumbing.UnexpectedError
	if errors.As(err, &gitErr) {
		if inner, ok := gitErr.Err.(interface{ StatusCode() int }); ok {
			return inner.StatusCode(), true
		}
	}

	// A HEAD response has no body, so the registry library returns an
	// unexported type; its message is the only handle.
	if m := statusText.FindStringSubmatch(err.Error()); m != nil {
		code, _ := strconv.Atoi(m[1])
		return code, true
	}
	return 0, false
}
