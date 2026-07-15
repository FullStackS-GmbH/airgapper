# Universal Airgapper

A Go CLI tool for synchronizing container images, Helm charts, and Git repositories across air-gapped environments.

<!-- Badges -->
![Build](https://img.shields.io/github/actions/workflow/status/fullstacks-gmbh/airgapper/ci.yml?branch=main)
![Release](https://img.shields.io/github/v/release/fullstacks-gmbh/airgapper)
![License](https://img.shields.io/github/license/fullstacks-gmbh/airgapper)

---

## What It Does

Universal Airgapper reads a YAML configuration file listing artifacts (container images, Helm charts, Git repos) with their source and destination, then copies each artifact from source to destination. It is designed for environments where registries sit behind an air gap and artifacts must be moved in a controlled, automated way.

## Features

- Sync **container images** between any Docker v2-compatible registries (Docker Hub, GHCR, Harbor, Nexus, private registries)
- Sync **Helm charts** between OCI registries and legacy HTTP chart repositories
- Sync **Git repositories** between any hosting services (GitHub, GitLab, Bitbucket, Azure Repos, self-hosted) via HTTPS or SSH
- **Helm image extraction** -- render Helm charts and extract all container image references, generating a ready-to-use image sync config for air-gapped registry migration
- **Regex patterns** for tags, chart versions, and git refs -- sync all matching versions automatically
- **Generic command scanner** -- run any external security/compliance tool before promoting an artifact
- **Push modes**: `skip` (default), `force`, `overwrite` -- control what happens when an artifact already exists at the destination
- **Dry-run mode** -- preview what would be synced without making any changes
- **Multi-file configuration** -- split config across multiple YAML files, merged at load time
- **Flexible credential management** -- file-based credentials, organized by type, with SSH key support
- **Multiple deployment targets**: run locally, in Docker, as a Kubernetes Job/CronJob, or in GitHub Actions / GitLab CI
- Structured JSON logging via `slog`
- Single statically linked binary, zero runtime dependencies

## Quick Start

### Install

Download the latest binary from [GitHub Releases](https://github.com/fullstacks-gmbh/airgapper/releases), or build from source:

```shell
# Build from source (requires Go 1.26.3+)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o airgapper ./cmd/airgapper
```

Or use the container image:

```shell
docker pull ghcr.io/fullstacks-gmbh/airgapper:latest
```

### Configure

Create a configuration file (e.g., `config.config.config.airgapper.yaml`):

```yaml
resources:
  - type: image
    source: ubuntu
    destination: registry.internal.corp/mirror/ubuntu
    tags:
      - "22.04"
      - "24.04"
```

### Run

```shell
# Local binary
airgapper sync --config ./config.config.airgapper.yaml

# Docker
docker run --rm \
  -v $(pwd)/config.config.airgapper.yaml:/mnt/config.config.airgapper.yaml \
  ghcr.io/fullstacks-gmbh/airgapper:latest \
  sync --config /mnt/
```

## Configuration Reference

Configuration files must be named `*.config.airgapper.yaml` or `*.config.airgapper.yml`.
When a folder is provided via `--config`, all matching files are discovered and merged (resource lists are concatenated; scanner definitions are merged by name).

See [docs/configuration.md](docs/configuration.md) for the full reference.

### Image Resources

Sync container images between Docker v2-compatible registries.

```yaml
resources:
  - type: image                # also accepts "docker" for backward compat
    source: registry.example.com/team/app
    destination: internal-registry.corp/mirror/app
    push_mode: skip            # skip | force (default: skip)
    scanner_ref: trivy         # optional: run scanner before push
    source_credentials_ref: source-reg   # optional
    target_credentials_ref: internal-reg # optional
    tags:
      - "v1.0.0"
      - "v2\\..*"             # regex: all v2.x tags
      - "latest"
```

### Helm Resources

Sync Helm charts between OCI registries and legacy HTTP chart repositories.

```yaml
resources:
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/postgresql
    destination_registry: internal-registry.corp
    destination_repo: charts
    push_mode: skip            # skip | overwrite (default: skip)
    versions:
      - "16.0.0"
      - "16\\..*"             # regex: all 16.x versions
```

### Git Resources

Sync Git repositories between hosting services via HTTPS or SSH.

```yaml
resources:
  - type: git
    source_repo: git@github.com:org/project.git
    destination_repo: git@internal-git.corp:mirror/project.git
    push_mode: skip            # skip | push | force (default: skip)
    source_credentials_ref: github-ssh        # optional
    target_credentials_ref: internal-git-ssh  # optional
    refs:
      - "main"
      - "release/.*"          # regex: all release branches
      - "v[0-9]+\\..*"        # regex: all version tags
```

### Scanner Configuration

Define external scanner commands that run before an artifact is pushed. The scanner is generic -- any command-line tool that returns an exit code can be used.

```yaml
scanners:
  - name: trivy
    command: "trivy image --exit-code 1 --severity HIGH,CRITICAL {registry}/{repository}:{tag}"
    success_code: 0            # expected exit code for "pass" (default: 0)
    timeout: 300               # max seconds (default: 300)

resources:
  - type: image
    source: ubuntu
    destination: internal-registry.corp/mirror/ubuntu
    scanner_ref: trivy         # references the scanner above
    tags:
      - "22.04"
```

**Available placeholders** in the scanner command:

| Placeholder    | Description                                                    |
|----------------|----------------------------------------------------------------|
| `{registry}`   | Registry hostname (e.g., `registry-1.docker.io`)               |
| `{repository}` | Repository path (e.g., `library/ubuntu`)                       |
| `{tag}`        | Tag / version / ref being synced                               |
| `{source}`     | Full source reference (e.g., `docker.io/library/ubuntu:22.04`) |
| `{type}`       | Resource type (`image`, `helm`, `git`)                         |

### Full Example

```yaml
scanners:
  - name: trivy
    command: "trivy image --exit-code 1 --severity HIGH,CRITICAL {registry}/{repository}:{tag}"
    success_code: 0
    timeout: 300

resources:
  # Container images
  - type: image
    source: ubuntu
    destination: registry.internal.corp/mirror/ubuntu
    scanner_ref: trivy
    tags:
      - "22.04"
      - "24.04"

  - type: image
    source: registry.example.com/team/app
    destination: registry.internal.corp/mirror/app
    push_mode: force
    tags:
      - "v1\\..*"

  # Helm charts
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/mariadb
    destination_registry: registry.internal.corp
    destination_repo: charts
    versions:
      - "20.2.1"
      - "20.2.0"

  # Git repositories
  - type: git
    source_repo: git@github.com:org/project.git
    destination_repo: git@internal-git.corp:mirror/project.git
    push_mode: skip
    refs:
      - "main"
      - "v.*"
```

## Credentials Reference

Credentials are provided in separate YAML files, organized by type.
Provide them via `--credentials` (single file or folder).
When pointing to a folder, files must match `*.creds.airgapper.yaml` or `*.creds.airgapper.yml`.

See [docs/credentials.md](docs/credentials.md) for the full reference.

```yaml
image:
  - name: docker-hub
    username: myuser
    password: mytoken
  - name: internal-registry
    username: serviceaccount
    password: s3cret

helm:
  - name: chart-registry
    username: helmuser
    password: helmpass

git:
  - name: github-ssh
    ssh_key_path: /path/to/id_ed25519
  - name: github-https
    username: git
    password: ghp_xxxxxxxxxxxx
  - name: azure-repos
    username: AzureReposAuthnSucks    # Azure Repos workaround
    password: REPO_AUTH_HEADER        # env var name containing the auth header
```

**Credential resolution order:**

1. By explicit reference (`source_credentials_ref` / `target_credentials_ref` in resource config)
2. By hostname match (credential `name` matches the registry/host)
3. Anonymous access (no credentials found)

## CLI Reference

### Commands

```
airgapper sync            Synchronize artifacts from source to destination
airgapper helm images     Extract container image references from Helm charts
airgapper version         Print version, commit, and build date
```

### Global Flags

| Flag            | Short | Env Var                 | Default | Description                              |
|-----------------|-------|-------------------------|---------|------------------------------------------|
| `--config`      | `-c`  | `AIRGAPPER_CONFIG`      | (none)  | Path to config file or folder            |
| `--credentials` |       | `AIRGAPPER_CREDENTIALS` | (none)  | Path to credentials file or folder       |
| `--debug`       | `-d`  | `AIRGAPPER_DEBUG`       | `false` | Enable debug logging (JSON, DEBUG level) |
| `--dry-run`     |       | `AIRGAPPER_DRY_RUN`     | `false` | Disable all write/push operations        |

### `helm images` Flags

| Flag                      | Env Var                                        | Required | Description                                                                 |
|---------------------------|------------------------------------------------|----------|-----------------------------------------------------------------------------|
| `--output`, `-o`          | `AIRGAPPER_HELM_IMAGES_OUTPUT`                 | yes      | Path to write the generated image config YAML                               |
| `--target-credentials-ref`| `AIRGAPPER_HELM_IMAGES_TARGET_CREDENTIALS_REF` | yes      | Name of a helm credential entry whose `name` field is the destination registry hostname |

The command reads all `helm` resources from the config, pulls and renders each chart version with its default values, extracts every `image:` reference from the rendered manifests, and writes a ready-to-use airgapper image config YAML to `--output`. The output file can be fed directly to `airgapper sync` to mirror those images into an air-gapped registry.

```shell
airgapper helm images \
  --config ./configs/ \
  --credentials ./creds/ \
  --output ./image-sync.config.airgapper.yaml \
  --target-credentials-ref my-internal-registry
```

### Exit Codes

| Code | Meaning                                  |
|------|------------------------------------------|
| `0`  | All resources synced successfully        |
| `1`  | Sync completed with one or more failures |
| `2`  | Usage or configuration error             |

### Examples

```shell
# Sync with a single config file
airgapper sync --config config.config.airgapper.yaml

# Sync with a folder of config files and credentials
airgapper sync --config ./configs/ --credentials ./creds/

# Dry run with debug logging
airgapper sync --config ./configs/ --credentials ./creds/ --dry-run --debug

# Using environment variables
export AIRGAPPER_CONFIG=./configs/
export AIRGAPPER_CREDENTIALS=./creds/
export AIRGAPPER_DRY_RUN=true
airgapper sync

# Print version
airgapper version
```

## Deployment Options

### Local

```shell
airgapper sync --config ./configs/ --credentials ./creds/
```

### Docker

```shell
docker run --rm \
  -v $(pwd)/configs:/mnt/configs \
  -v $(pwd)/creds:/mnt/creds \
  ghcr.io/fullstacks-gmbh/airgapper:latest \
  sync --config /mnt/configs/ --credentials /mnt/creds/
```

For Git over SSH, mount your SSH configuration:

```shell
docker run --rm \
  -v $(pwd)/.ssh:/home/nonroot/.ssh:ro \
  -v $(pwd)/configs:/mnt/configs \
  -v $(pwd)/creds:/mnt/creds \
  ghcr.io/fullstacks-gmbh/airgapper:latest \
  sync --config /mnt/configs/ --credentials /mnt/creds/
```

### Kubernetes Job

Mount config via ConfigMap and credentials via Secrets. Example manifests are in `k8s/`.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: airgapper-job
spec:
  template:
    spec:
      containers:
        - name: airgapper
          image: ghcr.io/fullstacks-gmbh/airgapper:latest
          command: [ "/airgapper" ]
          args: [ "sync", "--config", "/mnt/config/", "--credentials", "/mnt/creds/" ]
          volumeMounts:
            - name: config-volume
              mountPath: /mnt/config/config.yaml
              subPath: config.yaml
            - name: secrets-image
              mountPath: /mnt/creds/image.yaml
              subPath: image.yaml
      restartPolicy: Never
      volumes:
        - name: config-volume
          configMap:
            name: airgapper-config
        - name: secrets-image
          secret:
            secretName: airgapper-secrets-image
```

### Kubernetes CronJob

Same as Job, with a schedule:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: airgapper-cronjob
spec:
  schedule: "0 0,12 * * *"   # Twice daily
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: airgapper
              image: ghcr.io/fullstacks-gmbh/airgapper:latest
              command: [ "/airgapper" ]
              args: [ "sync", "--config", "/mnt/config/", "--credentials", "/mnt/creds/" ]
              # ... volume mounts as above
```

### GitHub Actions

Use the reusable workflow from this repository. See [docs/github-actions.md](docs/github-actions.md).

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    with:
      config-folder: ${{ github.workspace }}/configs
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
```

### GitLab CI

Use the CI component template. See [docs/gitlab-ci.md](docs/gitlab-ci.md).

```yaml
include:
  - component: <airgapper-repo-group>/airgapper@1
    inputs:
      stage: run
      config-folder: ${CI_PROJECT_DIR}
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
```

## Architecture Overview

Universal Airgapper follows **Hexagonal Architecture (Ports & Adapters)** with the Strategy and Factory patterns for pluggable transport backends.

```
cmd/airgapper/main.go        # Thin entrypoint
internal/
  cli/                        # Cobra commands (root, sync, helm images, version)
  config/                     # YAML config loading, validation, merging
  credentials/                # File-based credential store
  domain/                     # Core types, interfaces, errors (zero deps)
  helmimages/                 # Helm chart rendering and image reference extraction
  transport/
    image/                    # Container image sync (containers/image v5)
    helm/                     # Helm chart sync (Helm v4 SDK)
    git/                      # Git repo sync (go-git v5)
  scanner/                    # Generic external command scanner
  sync/                       # Orchestration engine
  logging/                    # Structured logging (slog)
```

For the full C4 architecture model and detailed design, see [docs/architecture/](docs/architecture/).

## Development

### Prerequisites

- Go 1.26.3+
- [direnv](https://direnv.net/) (optional, for automatic env setup)
- [Nix](https://nixos.org/) (optional, provides reproducible toolchain via `flake.nix`)
- Docker (for container builds)

### Build

```shell
# Development build
go build ./cmd/airgapper

# Production build (static, stripped)
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-s -w -X main.version=$(git describe --tags) -X main.commit=$(git rev-parse HEAD)" \
  -o airgapper \
  ./cmd/airgapper
```

### Test

```shell
go test ./...                # Unit tests
go test -race ./...          # With race detector
go test -tags=integration ./... # Integration tests
```

### Lint

```shell
golangci-lint run
```

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding standards, testing guidelines, and commit conventions.

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).
