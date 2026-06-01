# Stage 1: Build the Go binary
FROM golang:1.26.3-alpine AS builder

ARG APP_VERSION="dev"
ARG APP_COMMIT_SHA="unknown"

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy dependency files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .

ARG TARGETOS
ARG TARGETARCH
# Build tags required by go.podman.io/image/v5 (containers/container-libs).
# - containers_image_openpgp:        avoid gpgme CGO dep
# - exclude_graphdriver_*:           skip storage backends (only docker:// transport is used)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -tags=containers_image_openpgp,exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,exclude_graphdriver_overlay \
    -trimpath \
    -ldflags="-s -w -X main.version=${APP_VERSION} -X main.commit=${APP_COMMIT_SHA}" \
    -o /airgapper \
    ./cmd/airgapper

# Stage 2: CI runtime image (SUSE BCI hardened base)
FROM registry.suse.com/bci/bci-base:15.6

ARG APP_VERSION="dev"
ARG APP_COMMIT_SHA="unknown"

LABEL org.opencontainers.image.title="airgapper" \
      org.opencontainers.image.description="Universal Airgapper image with SUSE BCI hardened runtime" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${APP_COMMIT_SHA}" \
      org.opencontainers.image.source="https://github.com/fullstacks-gmbh/airgapper" \
      org.opencontainers.image.vendor="FULLSTACKS GmbH"

COPY --from=builder /airgapper /airgapper
RUN zypper --non-interactive ref && \
    zypper --non-interactive in --no-recommends \
      bash \
      ca-certificates \
      git-core \
      gzip \
      openssh-clients \
      tar && \
    zypper clean --all

RUN ln -sf /airgapper /usr/local/bin/airgapper
