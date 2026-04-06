# Configuration Reference

This document covers the full configuration format for Universal Airgapper.

## File Naming and Discovery

Configuration files must match one of these patterns:

- `*.airgapper.yaml`
- `*.airgapper.yml`
- `*.cnairgapper.yaml` (backward compatibility with the Python version)

When `--config` points to a **folder**, all matching files in that folder are discovered and merged. When it points to a **single file**, only that file is loaded.

## Multi-File Merging

When multiple config files are found:

- `resources` arrays are **concatenated** across all files.
- `scanners` arrays are **merged by name** -- if two files define a scanner with the same name, the last one wins (a warning is logged).

This allows you to organize configuration by concern:

```
configs/
  images.airgapper.yaml      # Container image resources
  helm.airgapper.yaml        # Helm chart resources
  git.airgapper.yaml         # Git repository resources
  scanners.airgapper.yaml    # Scanner definitions
```

## Top-Level Structure

```yaml
# Optional: scanner definitions
scanners:
  - name: <string>
    command: <string>
    success_code: <int>
    timeout: <int>

# Required: resources to synchronize
resources:
  - type: <string>
    # ... type-specific fields
```

## Resource Types

### Image (`type: image` or `type: docker`)

Sync container images between Docker v2-compatible registries. The type `docker` is accepted as an alias for `image`.

| Field                    | Required | Description |
|--------------------------|----------|-------------|
| `type`                   | yes      | `image` or `docker` |
| `source`                 | yes      | Source image reference (e.g., `ubuntu`, `registry.example.com/repo/image`) |
| `destination`            | yes      | Destination image reference |
| `tags`                   | yes      | List of tags to sync (supports regex patterns) |
| `push_mode`              | no       | `skip` (default) or `force` |
| `scanner_ref`            | no       | Name of a scanner to run before push |
| `source_credentials_ref` | no       | Name of a credential entry for the source |
| `target_credentials_ref` | no       | Name of a credential entry for the destination |

**Image name resolution** follows Docker conventions:

- `ubuntu` resolves to `registry-1.docker.io/library/ubuntu`
- `myrepo/myimage` resolves to `registry-1.docker.io/myrepo/myimage`
- `registry.example.com/myrepo/myimage` is used as-is

```yaml
resources:
  - type: image
    source: registry.example.com/team/app
    destination: internal-registry.corp/mirror/app
    push_mode: skip
    scanner_ref: trivy
    source_credentials_ref: source-reg
    target_credentials_ref: internal-reg
    tags:
      - "v1.0.0"
      - "v1.0.1"
      - "v2\\..*"         # Regex: matches all v2.x tags
```

### Helm (`type: helm`)

Sync Helm charts between OCI registries and legacy HTTP chart repositories.

| Field                    | Required | Description |
|--------------------------|----------|-------------|
| `type`                   | yes      | `helm` |
| `source_registry`        | yes      | Source registry hostname |
| `source_chart`           | yes      | Chart name within source registry (e.g., `bitnamicharts/mariadb`) |
| `destination_registry`   | yes      | Destination registry hostname |
| `destination_repo`       | yes      | Repository path in destination registry |
| `versions`               | yes      | List of chart versions to sync (supports regex patterns) |
| `push_mode`              | no       | `skip` (default) or `overwrite` |
| `source_credentials_ref` | no       | Name of a credential entry for the source |
| `target_credentials_ref` | no       | Name of a credential entry for the destination |

```yaml
resources:
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/postgresql
    destination_registry: internal-registry.corp
    destination_repo: charts
    push_mode: skip
    versions:
      - "16.0.0"
      - "16\\..*"         # Regex: matches all 16.x versions
```

### Git (`type: git`)

Sync Git repositories between hosting services via HTTPS or SSH.

| Field                    | Required | Description |
|--------------------------|----------|-------------|
| `type`                   | yes      | `git` |
| `source_repo`            | yes      | Source repository URL (HTTPS or SSH) |
| `destination_repo`       | yes      | Destination repository URL (HTTPS or SSH) |
| `refs`                   | yes      | List of refs to sync: branches, tags, SHAs (supports regex patterns) |
| `push_mode`              | no       | `skip` (default), `push`, or `force` |
| `source_credentials_ref` | no       | Name of a credential entry for the source |
| `target_credentials_ref` | no       | Name of a credential entry for the destination |

**Push modes for git:**

| Mode    | Behavior |
|---------|----------|
| `skip`  | Skip if the ref already exists at the destination |
| `push`  | Push the ref (fails if the remote rejects it, e.g., non-fast-forward) |
| `force` | Force push the ref (overwrites the remote ref unconditionally) |

```yaml
resources:
  - type: git
    source_repo: git@github.com:org/project.git
    destination_repo: git@internal-git.corp:mirror/project.git
    push_mode: skip
    source_credentials_ref: github-ssh
    target_credentials_ref: internal-git-ssh
    refs:
      - "main"
      - "release/.*"      # Regex: all release branches
      - "v[0-9]+\\..*"    # Regex: all version tags
```

## Scanner Configuration

Scanners are external commands that run before an artifact is pushed to the destination. They are defined in the `scanners` section and referenced by name in resources via `scanner_ref`.

| Field          | Required | Default | Description |
|----------------|----------|---------|-------------|
| `name`         | yes      |         | Unique identifier, referenced by `scanner_ref` |
| `command`      | yes      |         | Shell command template with placeholders |
| `success_code` | no       | `0`     | Exit code that indicates a passing scan |
| `timeout`      | no       | `300`   | Maximum execution time in seconds |

### Command Placeholders

The command string supports placeholders that are substituted with artifact metadata at runtime:

| Placeholder    | Substituted With |
|----------------|------------------|
| `{registry}`   | Registry hostname (e.g., `registry-1.docker.io`) |
| `{repository}` | Repository path (e.g., `library/ubuntu`) |
| `{tag}`        | Tag, version, or ref being synced |
| `{source}`     | Full source reference (e.g., `registry-1.docker.io/library/ubuntu:22.04`) |
| `{type}`       | Resource type (`image`, `helm`, `git`) |

### Examples

```yaml
scanners:
  # Trivy vulnerability scanner
  - name: trivy
    command: "trivy image --exit-code 1 --severity HIGH,CRITICAL {registry}/{repository}:{tag}"
    success_code: 0
    timeout: 300

  # Snyk container test
  - name: snyk
    command: "snyk container test {source} --severity-threshold=high"
    success_code: 0
    timeout: 600

  # Custom compliance check
  - name: compliance
    command: "/opt/tools/check-policy --image {source} --type {type}"
    success_code: 0
    timeout: 120
```

## Regex Patterns for Tags, Versions, and Refs

A version/tag/ref string is treated as a **regex pattern** if it contains any of these characters: `*`, `+`, `[`, `]`, `{`, `}`, `(`, `)`, `\`, `^`, `$`, `|`, `?`.

When a pattern is detected:

1. The tool calls `ListVersions()` to get all available versions from the source.
2. The pattern is compiled as a Go `regexp`.
3. Only full matches are returned (the pattern is anchored with `^` and `$`).
4. Matched versions are added to the sync list.

### Examples

| Pattern        | Matches |
|----------------|---------|
| `v1\\..*`      | `v1.0.0`, `v1.2.3`, `v1.99.0` |
| `v[0-9]+\\..*` | Any tag starting with `v` followed by digits and a dot |
| `release/.*`   | `release/1.0`, `release/hotfix`, etc. |
| `main`         | Literal match: only `main` |
| `"22.04"`      | Literal match: only `22.04` (no regex metacharacters) |

## Full Example

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
      - "latest"

  - type: image
    source: registry.example.com/team/app
    destination: registry.internal.corp/mirror/app
    push_mode: force
    source_credentials_ref: source-reg
    target_credentials_ref: internal-reg
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
      - "20.1.1"

  - type: helm
    source_registry: neuvector.github.io/neuvector-helm
    source_chart: core
    destination_registry: registry.internal.corp
    destination_repo: charts/neuvector
    versions:
      - "2\\..*"

  # Git repositories
  - type: git
    source_repo: git@github.com:org/project.git
    destination_repo: git@internal-git.corp:mirror/project.git
    push_mode: skip
    refs:
      - "main"
      - "release/.*"
      - "v[0-9]+\\..*"

  - type: git
    source_repo: https://github.com/org/another-project.git
    destination_repo: https://internal-git.corp/mirror/another-project.git
    push_mode: push
    refs:
      - "main"
      - "v.*"
```
