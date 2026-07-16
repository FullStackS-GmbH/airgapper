# GitHub Actions Integration

Universal Airgapper provides a reusable workflow that you can call from any GitHub Actions pipeline.

## Reusable Workflow

The workflow is located at `.github/workflows/universal-airgapper.yml` in this repository. It runs the airgapper container image, checks out your repository, writes credentials from a secret, and executes the sync.

## How to Use

Call the reusable workflow from your repository's workflow file:

```yaml
name: Sync Artifacts

on:
  workflow_dispatch:           # Manual trigger
  schedule:
    - cron: "0 0,12 * * *"    # Twice daily

jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    with:
      config-folder: ${{ github.workspace }}/configs
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
      AIRGAPPER_CREDENTIALS: ${{ secrets.UNIVERSAL_AIRGAPPER_CREDS }}
```

## Inputs

All inputs are optional and have sensible defaults.

| Input                     | Type   | Default                               | Description                                               |
|---------------------------|--------|---------------------------------------|-----------------------------------------------------------|
| `job-name`                | string | `airgapper`                           | Display name for the job in the Actions UI                |
| `image_registry`          | string | `ghcr.io`                             | Container registry hosting the airgapper image            |
| `image_name`              | string | `fullstacks-gmbh/airgapper`           | Image name (without registry or tag)                      |
| `image_tag`               | string | `latest`                              | Image tag to use                                          |
| `image_pull_policy`       | string | `always`                              | Image pull policy (`always`, `if-not-present`, `never`)   |
| `config-folder`           | string | (workspace)                           | Path to folder containing `*.config.airgapper.yaml` files |
| `debug`                   | string | `""`                                  | Set to `--debug` to enable debug logging                  |
| `credentials-secret-name` | string | `UNIVERSAL_AIRGAPPER_CREDS`           | Legacy secret lookup when `AIRGAPPER_CREDENTIALS` is not mapped |

## Secrets

| Secret                  | Required | Description                                                                                            |
|-------------------------|----------|--------------------------------------------------------------------------------------------------------|
| `GHCR_PAT`              | yes      | Personal access token with `read:packages` scope, used to pull the airgapper container image from GHCR |
| `AIRGAPPER_CREDENTIALS` | no       | Full airgapper credential YAML; preferred over dynamic secret lookup                                  |

Map the repository secret containing the credential YAML to the reusable workflow's `AIRGAPPER_CREDENTIALS` secret, as shown in the examples.

## How It Works

1. The job runs on `ubuntu-latest` inside the airgapper container image.
2. It checks out your repository (so config files in the repo are available).
3. It validates and lists the matching config files without printing their contents.
4. It writes the credential secret to a permission-restricted temporary file.
5. It runs `/airgapper sync` with the provided config folder and credentials file.

## Examples

### Minimal

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
      AIRGAPPER_CREDENTIALS: ${{ secrets.UNIVERSAL_AIRGAPPER_CREDS }}
```

Config files are expected at the repository root (`*.config.airgapper.yaml`). Credentials are read from the `UNIVERSAL_AIRGAPPER_CREDS` secret.

### With Custom Config Folder and Debug

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    with:
      config-folder: ${{ github.workspace }}/airgapper-configs
      debug: "--debug"
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
      AIRGAPPER_CREDENTIALS: ${{ secrets.UNIVERSAL_AIRGAPPER_CREDS }}
```

### With Custom Image Tag

Pin to a specific release instead of `latest`:

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    with:
      image_tag: "1.0.0"
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
      AIRGAPPER_CREDENTIALS: ${{ secrets.UNIVERSAL_AIRGAPPER_CREDS }}
```

### With a Different Credentials Secret

If your credential YAML is stored in a differently named secret:

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
      AIRGAPPER_CREDENTIALS: ${{ secrets.MY_AIRGAPPER_CREDS }}
```

## Setting Up Secrets

### GHCR_PAT

Create a personal access token with `read:packages` scope at [GitHub Settings > Developer settings > Personal access tokens](https://github.com/settings/tokens). Add it as a repository secret named `GHCR_PAT`.

### UNIVERSAL_AIRGAPPER_CREDS

Store your credential YAML as a repository secret. The full YAML content goes into the secret value:

```yaml
image:
  - name: registry.example.com
    username: myuser
    password: mytoken
helm:
  - name: chart-registry.example.com
    username: helmuser
    password: helmpass
git:
  - name: github.com
    username: git
    password: ghp_xxxxxxxxxxxx
```

Go to **Settings > Secrets and variables > Actions > New repository secret**, name it `UNIVERSAL_AIRGAPPER_CREDS`, and paste the YAML content.
