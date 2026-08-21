package registry

import (
	"context"
	"errors"
	"log/slog"

	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/types"
)

// ManifestExists reports whether a manifest is present at ref.
//
// A tag that is genuinely absent yields (false, nil), so the caller proceeds to
// copy it. Errors meaning the question could not be answered — rejected
// credentials, rate limiting — yield (false, err): reporting those as "not
// present" would let push_mode=skip start a copy that then fails for a reason
// the operator never saw. Anything else (network blips, odd registry
// responses) is logged and treated as absent; the copy that follows surfaces
// the real error.
func ManifestExists(ctx context.Context, sys *types.SystemContext, ref types.ImageReference, logger *slog.Logger) (bool, error) {
	_, err := docker.GetDigest(ctx, sys, ref)
	if err == nil {
		return true, nil
	}

	var unauthorized docker.ErrUnauthorizedForCredentials
	if errors.As(err, &unauthorized) || errors.Is(err, docker.ErrTooManyRequests) {
		return false, err
	}

	if logger != nil {
		logger.Debug("existence check failed; treating as not present",
			slog.String("ref", ref.StringWithinTransport()),
			slog.String("error", err.Error()),
		)
	}
	return false, nil
}
