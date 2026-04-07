# Manual integration testing

Run the airgapper against live Harbor and GitLab instances using realistic configs that cover multiple source registries, regex patterns, and artifact types.

## What this test covers

| Use-case         | Source                                                          | Destination             | Pattern types             |
|------------------|-----------------------------------------------------------------|-------------------------|---------------------------|
| Container images | Docker Hub, Quay.io, GHCR.io                                    | Harbor project `ddrack` | Specific tags + regex     |
| Git repository   | `github.com/juice-shop/juice-shop`                              | Private GitLab group    | Regex semver tags         |
| Helm charts      | Bitnami OCI (Docker Hub), JFrog Artifactory (`charts.jfrog.io`) | Harbor project `ddrack` | Specific versions + regex |

## Prerequisites

- A Harbor registry with API access and permission to create and delete projects.
- A GitLab instance with API access and permission to create and delete projects in a group.
- The `airgapper` binary (build with `go build -o airgapper ./cmd/airgapper` if needed).
- `curl` and `python3` (used by `run.sh` for API calls and JSON parsing).

## Environment variables

Export these before running any commands.

| Variable          | Description                                         | Example                               |
|-------------------|-----------------------------------------------------|---------------------------------------|
| `HARBOR_URL`      | Harbor base URL                                     | `https://registry.lab.cloudstacks.eu` |
| `HARBOR_USERNAME` | Harbor username                                     | `user@example.com`                    |
| `HARBOR_PASSWORD` | Harbor password or robot token                      | `secret`                              |
| `GITLAB_URL`      | GitLab base URL                                     | `https://gitlab.com`                  |
| `GITLAB_TOKEN`    | GitLab personal access token (api scope)            | `glpat-xxxx`                          |
| `GITLAB_GROUP_ID` | Numeric ID of the GitLab group for the test project | `12345`                               |

Optional overrides:

| Variable         | Default                        | Description                                           |
|------------------|--------------------------------|-------------------------------------------------------|
| `HARBOR_PROJECT` | `ddrack`                       | Harbor project name to create                         |
| `GITLAB_PROJECT` | `airgapper-juice-shop`         | GitLab project name to create                         |
| `STATE_FILE`     | `tests/integration/.state.env` | Path to persist project IDs between setup and cleanup |

## Workflow

The test is a three-step manual process: create infrastructure, run the airgapper, then tear down.

### 1. Set credentials

```bash
export HARBOR_URL=https://registry.lab.cloudstacks.eu
export HARBOR_USERNAME=your-user
export HARBOR_PASSWORD=your-password
export GITLAB_URL=https://gitlab.com
export GITLAB_TOKEN=glpat-xxxx
export GITLAB_GROUP_ID=12345
```

### 2. Create infrastructure

```bash
./tests/integration/run.sh setup
```

This command:

- Creates the Harbor project (default: `ddrack`), or reuses it if it already exists.
- Creates the GitLab project (default: `airgapper-juice-shop`) in the specified group, or reuses it if it already exists.
- Writes rendered credential files into `tests/integration/creds/` with your actual secrets.
- Writes the rendered git config with the real GitLab project path into `tests/integration/configs/git.config.airgapper.yaml`.
- Saves project IDs to a state file so cleanup can find them later.

### 3. Run the airgapper

Run all configs at once:

```bash
./airgapper sync \
  --config tests/integration/configs/ \
  --credentials tests/integration/creds/ \
  --debug
```

Or run each use-case individually:

```bash
# Images only (Docker Hub, Quay, GHCR → Harbor)
./airgapper sync \
  --config tests/integration/configs/images.config.airgapper.yaml \
  --credentials tests/integration/creds/ \
  --debug

# Git only (Juice Shop → GitLab)
./airgapper sync \
  --config tests/integration/configs/git.config.airgapper.yaml \
  --credentials tests/integration/creds/ \
  --debug

# Helm only (Bitnami + JFrog → Harbor)
./airgapper sync \
  --config tests/integration/configs/helm.config.airgapper.yaml \
  --credentials tests/integration/creds/ \
  --debug
```

Add `--dry-run` to preview what would be synced without writing anything:

```bash
./airgapper sync \
  --config tests/integration/configs/ \
  --credentials tests/integration/creds/ \
  --dry-run --debug
```

### 4. Clean up

```bash
./tests/integration/run.sh cleanup
```

This command:

- Deletes the Harbor project and all its repositories.
- Deletes the GitLab project.
- Removes the state file.

## File layout

```
tests/integration/
├── README.md                              # This file
├── run.sh                                 # Setup and cleanup script
├── configs/
│   ├── images.config.airgapper.yaml       # Docker Hub + Quay + GHCR images
│   ├── git.config.airgapper.yaml          # Juice Shop git mirror
│   └── helm.config.airgapper.yaml         # Bitnami OCI + JFrog Artifactory charts
└── creds/
    ├── image.creds.airgapper.yaml         # Harbor image registry credentials
    ├── helm.creds.airgapper.yaml          # Harbor helm registry credentials
    └── git.creds.airgapper.yaml           # GitLab HTTPS credentials
```

## Config details

### Container images (`images.config.airgapper.yaml`)

| Source     | Image                               | Tags                        | Pattern type     |
|------------|-------------------------------------|-----------------------------|------------------|
| Docker Hub | `alpine`                            | `3.20`, `3.21`, `3\.2[0-9]` | Specific + regex |
| Docker Hub | `busybox`                           | `1.37`                      | Specific         |
| Quay.io    | `prometheus/node-exporter`          | `v1.8.2`, `v1\.8\.[0-9]+`   | Specific + regex |
| Quay.io    | `argoproj/argocd`                   | `v2.13.3`                   | Specific         |
| GHCR.io    | `external-secrets/external-secrets` | `v0.14.3`, `v0\.14\.[0-9]+` | Specific + regex |

All images are pushed to `registry.lab.cloudstacks.eu/ddrack/<image-name>` with `push_mode: skip`.

### Git repository (`git.config.airgapper.yaml`)

| Source                             | Destination          | Refs                              | Pattern type               |
|------------------------------------|----------------------|-----------------------------------|----------------------------|
| `github.com/juice-shop/juice-shop` | GitLab group project | `main`, `v[0-9]+\.[0-9]+\.[0-9]+` | Branch + regex semver tags |

Uses `push_mode: force` because the destination starts empty.
The `run.sh setup` command renders this file with the actual GitLab project path.

### Helm charts (`helm.config.airgapper.yaml`)

| Source                          | Chart         | Versions                            | Pattern type     |
|---------------------------------|---------------|-------------------------------------|------------------|
| Docker Hub OCI (Bitnami)        | `nginx`       | `18.3.5`, `18.3.4`, `18\.3\.[0-9]+` | Specific + regex |
| Docker Hub OCI (Bitnami)        | `redis`       | `20.11.3`                           | Specific         |
| `charts.jfrog.io` (Artifactory) | `artifactory` | `107.98.14`, `107.98.13`            | Specific         |
| `charts.jfrog.io` (Artifactory) | `xray`        | `104.98.6`                          | Specific         |

All charts are pushed to `registry.lab.cloudstacks.eu/ddrack/charts/` (Bitnami) or `ddrack/charts/jfrog/` (JFrog) with `push_mode: skip`.

## Credential resolution

The credential files use hostname-based matching.
No explicit `source_credentials_ref` or `target_credentials_ref` is needed because the credential `name` fields match the destination registry hostname.

| File                               | Type    | Matched host                  | Auth method            |
|------------------------------------|---------|-------------------------------|------------------------|
| `creds/image.creds.airgapper.yaml` | `image` | `registry.lab.cloudstacks.eu` | Username + password    |
| `creds/helm.creds.airgapper.yaml`  | `helm`  | `registry.lab.cloudstacks.eu` | Username + password    |
| `creds/git.creds.airgapper.yaml`   | `git`   | `gitlab.com`                  | OAuth2 token via HTTPS |

Source registries (Docker Hub, Quay, GHCR, GitHub, `charts.jfrog.io`) use anonymous access since all source artifacts are public.

## Troubleshooting

| Symptom                                            | Cause                                           | Fix                                                                                      |
|----------------------------------------------------|-------------------------------------------------|------------------------------------------------------------------------------------------|
| `run.sh setup` fails with HTTP 409 on Harbor       | Project already exists                          | Safe to ignore; the script reuses existing projects                                      |
| `run.sh setup` fails on GitLab create              | Token lacks `api` scope or wrong group ID       | Verify `GITLAB_TOKEN` scopes and `GITLAB_GROUP_ID`                                       |
| Airgapper fails with `unauthorized` on Harbor push | Wrong credentials or project does not exist     | Re-run `run.sh setup`; check `HARBOR_USERNAME` and `HARBOR_PASSWORD`                     |
| Git sync fails with `repository not found`         | GitLab project was not created or path is wrong | Check that `run.sh setup` completed and inspect `configs/git.config.airgapper.yaml`      |
| Helm sync fails on `charts.jfrog.io`               | JFrog may rate-limit or require auth            | Retry after a few minutes; add JFrog creds if needed                                     |
| `run.sh cleanup` cannot find GitLab project        | State file missing or was manually deleted      | The script searches by name as a fallback; verify `GITLAB_PROJECT` and `GITLAB_GROUP_ID` |
