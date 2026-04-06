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
```

## Inputs

All inputs are optional and have sensible defaults.

| Input                     | Type   | Default                               | Description                                               |
|---------------------------|--------|---------------------------------------|-----------------------------------------------------------|
| `job-name`                | string | `airgapper`                           | Display name for the job in the Actions UI                |
| `image_registry`          | string | `ghcr.io`                             | Container registry hosting the airgapper image            |
| `image_name`              | string | `fullstacks-gmbh/universal-airgapper` | Image name (without registry or tag)                      |
| `image_tag`               | string | `latest`                              | Image tag to use                                          |
| `image_pull_policy`       | string | `always`                              | Image pull policy (`always`, `if-not-present`, `never`)   |
| `config-folder`           | string | (workspace)                           | Path to folder containing `*.airgapper.yaml` config files |
| `debug`                   | string | `""`                                  | Set to `--debug` to enable debug logging                  |
| `credentials-secret-name` | string | `UNIVERSAL_AIRGAPPER_CREDS`           | Name of the GitHub secret containing credential YAML      |

## Secrets

| Secret     | Required | Description                                                                                            |
|------------|----------|--------------------------------------------------------------------------------------------------------|
| `GHCR_PAT` | yes      | Personal access token with `read:packages` scope, used to pull the airgapper container image from GHCR |

The credential YAML content is read from the secret specified by `credentials-secret-name` (default: `UNIVERSAL_AIRGAPPER_CREDS`). Store your full credential YAML as the value of that secret.

## How It Works

1. The job runs on `ubuntu-latest` inside the airgapper container image.
2. It checks out your repository (so config files in the repo are available).
3. It prints the config files for visibility (debug aid).
4. It writes the credential secret to a temporary file (`credentials.yaml`).
5. It runs `/airgapper sync` with the provided config folder and credentials file.

## Examples

### Minimal

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
```

Config files are expected at the repository root (`*.airgapper.yaml`). Credentials are read from the `UNIVERSAL_AIRGAPPER_CREDS` secret.

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
```

### With Custom Credentials Secret Name

If your credential YAML is stored in a differently named secret:

```yaml
jobs:
  sync:
    uses: fullstacks-gmbh/universal-airgapper/.github/workflows/universal-airgapper.yml@main
    with:
      credentials-secret-name: MY_AIRGAPPER_CREDS
    secrets:
      GHCR_PAT: ${{ secrets.GHCR_PAT }}
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
