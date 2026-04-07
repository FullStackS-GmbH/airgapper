# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${APP_VERSION} -X main.commit=${APP_COMMIT_SHA}" \
    -o /airgapper \
    ./cmd/airgapper

# Stage 2: Minimal runtime image
FROM gcr.io/distroless/static-debian12:nonroot

ARG APP_VERSION="dev"
ARG APP_COMMIT_SHA="unknown"

LABEL org.opencontainers.image.title="airgapper" \
      org.opencontainers.image.description="Universal Airgapper - sync artifacts across air-gapped environments" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${APP_COMMIT_SHA}" \
      org.opencontainers.image.source="https://github.com/fullstacks-gmbh/airgapper" \
      org.opencontainers.image.vendor="FULLSTACKS GmbH"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /airgapper /airgapper

ENTRYPOINT ["/airgapper"]
