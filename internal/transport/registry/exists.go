package registry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/types"
)

// ManifestExists reports whether a manifest is present at ref.
//
// A tag that is genuinely absent (HTTP 404) yields (false, nil), so the caller
// proceeds to copy it. Every other failure — rejected credentials, rate
// limiting, a 5xx, a network error — yields (false, err): reporting those as
// "not present" would let push_mode=skip overwrite a tag that does exist.
func ManifestExists(ctx context.Context, sys *types.SystemContext, ref types.ImageReference, logger *slog.Logger) (bool, error) {
	_, err := docker.GetDigest(ctx, sys, ref)
	if err == nil {
		return true, nil
	}
	if !isNotFound(err) {
		return false, err
	}
	if logger != nil {
		logger.Debug("manifest not found at destination",
			slog.String("ref", ref.StringWithinTransport()),
		)
	}
	return false, nil
}

// isNotFound reports whether err is a registry HTTP 404. A HEAD response has no
// body, so the library returns an unexported error type for it; the only handle
// is the "StatusCode: 404" text it puts in the message.
func isNotFound(err error) bool {
	var status docker.UnexpectedHTTPStatusError
	if errors.As(err, &status) {
		return status.StatusCode == http.StatusNotFound
	}
	return strings.Contains(err.Error(), "StatusCode: 404")
}
