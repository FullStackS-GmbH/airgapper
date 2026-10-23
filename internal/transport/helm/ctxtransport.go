package helm

import (
	"context"
	"io"
	"net/http"
)

// ctxTransport makes every request of a Helm registry client follow the run
// context. The Helm SDK's login, pull and push calls use a background context
// internally, so --timeout and cancellation would not otherwise reach them.
type ctxTransport struct {
	base http.RoundTripper
	ctx  context.Context
}

func (t ctxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, context.Cause(t.ctx)
	}

	// The request ends when either its own context or the run context ends.
	reqCtx, cancel := context.WithCancelCause(req.Context())
	stop := context.AfterFunc(t.ctx, func() { cancel(context.Cause(t.ctx)) })
	release := func() {
		stop()
		cancel(nil)
	}

	resp, err := t.base.RoundTrip(req.WithContext(reqCtx))
	if err != nil {
		release()
		return nil, err
	}
	// The body is read after RoundTrip returns, so the context must outlive it.
	resp.Body = &releaseBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

// releaseBody frees the request context once the body is closed.
type releaseBody struct {
	io.ReadCloser
	release func()
}

func (b *releaseBody) Close() error {
	err := b.ReadCloser.Close()
	b.release()
	return err
}
