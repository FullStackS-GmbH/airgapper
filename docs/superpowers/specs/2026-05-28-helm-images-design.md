# Design: `airgapper helm images` command

**Date:** 2026-05-28
**Status:** Approved

## Summary

Add a new `helm images` subcommand that reads helm resources from an airgapper config, pulls each chart version, renders it with the Helm SDK, extracts all container image references from the rendered manifests, and writes a ready-to-use airgapper image config file — with per-tag comments showing which helm chart each image tag came from.

---

## Command

```
airgapper helm images \
  --config ./charts.config.airgapper.yaml \
  --credentials ./creds.yaml \
  --target-credentials-ref helm-reg \
  --output helm-images.config.airgapper.yaml
```

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--config` / `-c` | yes | Inherited from root. Path to config file or folder. |
| `--credentials` | no | Inherited from root. Path to credentials file or folder. |
| `--target-credentials-ref` | yes | Name of a helm credential entry. Used as the destination registry prefix (credential `name` = registry hostname) and written as `target_credentials_ref` in every generated image entry. |
| `--output` / `-o` | yes | Path to write the generated image config YAML. |

---

## CLI Structure

A new `helm` parent command is registered on the root, with `images` as its first subcommand. This leaves room for future `helm`-scoped commands (e.g. `helm lint`).

New files:
- `internal/cli/helm.go` — `helm` parent command, registered in `root.go`
- `internal/cli/helm_images.go` — `images` subcommand handler
- `internal/helmimages/extractor.go` — chart pulling, rendering, and image extraction logic

---

## Image Extraction Flow

For each `type: helm` resource in the config, for each version:

1. **Pull** the chart tarball using the existing helm transport pull logic (reusing `internal/transport/helm`). Source credentials are resolved from each resource's `source_credentials_ref` field, same as `sync` does.
2. **Load** the tarball as a chart object via `helm.sh/helm/v4/pkg/chart/loader.LoadArchive()`.
3. **Render** the chart with default values via `helm.sh/helm/v4/pkg/engine.Engine{}.Render()` and `helm.sh/helm/v4/pkg/chartutil.ToRenderValues()`.
4. **Extract** all `image:` field values from the rendered Kubernetes manifest strings (walk YAML, match `image:` keys).
5. **Parse** each image reference into `{registry, repository, tag}`. Normalize short Docker Hub references (e.g. `nginx` → `docker.io/library/nginx`).
6. **Tag** each extracted image with its source chart name and chart version (e.g. `bitnamicharts/nginx:18.3.5`).

If rendering fails (e.g. chart requires custom `--set` values), log a warning and skip that version — do not abort the entire run.

After processing all resources:

- **Deduplicate** by `registry/repository`. Merge tags for the same source image into a single entry.
- A tag that appears in multiple chart versions accumulates all source labels.

---

## Output Format

The output is a valid airgapper config YAML written to `--output`. Images are grouped by source (`registry/repository`), one entry per unique image. Tags carry inline comments listing which helm chart version(s) they came from.

```yaml
resources:
  - type: image
    source: docker.io/bitnami/nginx
    destination: registry.internal.corp/bitnami/nginx
    target_credentials_ref: helm-reg
    tags:
      - "1.27.3-debian-12-r0" # from helm: bitnamicharts/nginx:18.3.5
      - "1.27.2-debian-12-r0" # from helm: bitnamicharts/nginx:18.3.4

  - type: image
    source: docker.io/bitnami/git
    destination: registry.internal.corp/bitnami/git
    target_credentials_ref: helm-reg
    tags:
      - "2.47.2-debian-12-r0" # from helm: bitnamicharts/nginx:18.3.5, bitnamicharts/redis:20.11.3
      - "2.47.1-debian-12-r0" # from helm: bitnamicharts/nginx:18.3.4
```

### Destination derivation

Given `--target-credentials-ref helm-reg` where the credential's `name` is `registry.internal.corp`:

- Source `docker.io/bitnami/nginx` → strip source registry → `bitnami/nginx`
- Destination: `registry.internal.corp/bitnami/nginx`

The credential `name` field is used as the registry hostname. By convention, airgapper credentials are named after their registry hostname (see credential resolution docs). The same name is written as `target_credentials_ref` in every generated entry.

---

## Error Handling

- Chart render failure → log warning with chart name and version, skip that version, continue.
- No helm resources in config → exit with a clear message (not an error, just informational).
- `--target-credentials-ref` names a credential not found in the credentials file → fail with a clear error before processing any charts.
- `--output` file already exists → overwrite (standard CLI behaviour; user controls filename).

---

## Testing

- Unit tests for `internal/helmimages/extractor.go`: image parsing, normalization, deduplication, comment annotation.
- Integration test config: `tests/integration/configs/helm.config.airgapper.yaml` (already exists with real chart entries) used as input fixture.
- Golden file test for output YAML shape (or table-driven tests comparing extracted image sets).
