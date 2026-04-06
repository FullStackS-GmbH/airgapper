.PHONY: build test lint fmt vet clean release-snapshot docker

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
IMAGE   ?= ghcr.io/fullstacks-gmbh/universal-airgapper

## build: Build the binary for the current platform
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o airgapper ./cmd/airgapper

## test: Run all tests with race detection
test:
	go test -race ./...

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## fmt: Format all Go source files
fmt:
	gofmt -w .
	goimports -w .

## vet: Run go vet
vet:
	go vet ./...

## release-snapshot: Build multi-arch binaries locally via goreleaser (no publish)
release-snapshot:
	goreleaser release --snapshot --clean

## docker: Build multi-arch Docker image locally (no push)
docker:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg APP_VERSION=$(VERSION) \
		--build-arg APP_COMMIT_SHA=$(COMMIT) \
		-f docker/Dockerfile \
		-t $(IMAGE):$(VERSION) \
		.

## docker-push: Build and push multi-arch Docker image to GHCR
docker-push:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg APP_VERSION=$(VERSION) \
		--build-arg APP_COMMIT_SHA=$(COMMIT) \
		-f docker/Dockerfile \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		--push \
		.

## clean: Remove build artifacts
clean:
	rm -rf airgapper dist/

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
