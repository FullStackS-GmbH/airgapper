# GitLab CI Integration

Universal Airgapper provides a GitLab CI [component template](https://docs.gitlab.com/ee/ci/components/) that can be included in any GitLab CI pipeline.

## Component Template

The template is located at `templates/airgapper.yml` in this repository. It defines a single job that runs the airgapper container image and executes the sync command.

## How to Use

Include the component in your `.gitlab-ci.yml`:

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: run
      config-folder: ${CI_PROJECT_DIR}
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
```

## Inputs

| Input               | Type   | Default                        | Options         | Description                                    |
|---------------------|--------|--------------------------------|-----------------|------------------------------------------------|
| `stage`             | string | `run`                          |                 | Pipeline stage for the job                     |
| `job-name`          | string | `airgapper`                    |                 | Job name in the pipeline                       |
| `image_registry`    | string | `ghcr.io`                      |                 | Container registry hosting the airgapper image |
| `image_name`        | string | `fullstacks-gmbh/airgapper-ci` |                 | Image name (without registry or tag)           |
| `image_tag`         | string | `latest`                       |                 | Image tag to use                               |
| `image_pull_policy` | string | `always`                       |                 | Image pull policy                              |
| `config-folder`     | string | `${CI_PROJECT_DIR}`            |                 | Path to folder containing config files         |
| `credentials-file`  | string | `${UNIVERSAL_AIRGAPPER_CREDS}` |                 | Path to credentials file                       |
| `debug`             | string | `""`                           | `""`, `--debug` | Set to `--debug` to enable debug logging       |

## CI Variables

The template expects the following GitLab CI variable to be set:

| Variable                    | Required | Description                                                         |
|-----------------------------|----------|---------------------------------------------------------------------|
| `UNIVERSAL_AIRGAPPER_CREDS` | yes      | Path to the credentials YAML file (typically a CI/CD file variable) |

### Setting Up the Credentials Variable

1. Go to **Settings > CI/CD > Variables** in your GitLab project (or group).
2. Add a new variable:
    - **Key**: `UNIVERSAL_AIRGAPPER_CREDS`
    - **Type**: File
    - **Value**: paste your full credential YAML content
    - **Protect variable**: recommended (limits to protected branches)
    - **Mask variable**: not possible for file variables, but the file is temporary

The file variable creates a temporary file during the pipeline and sets the env var to the file path.

## What the Job Does

1. Pulls the airgapper container image from the configured registry.
2. Prints the config files for visibility (debug aid).
3. Runs `/airgapper sync` with the provided config folder, credentials file, and optional debug flag.
4. The job uses `rules: - when: always`, so it runs on every pipeline unless overridden.

## Examples

### Minimal

Place your `*.airgapper.yaml` config files in the repository root and set the `UNIVERSAL_AIRGAPPER_CREDS` CI variable:

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: run
```

### With Custom Config Folder

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: sync
      config-folder: ${CI_PROJECT_DIR}/airgapper-configs
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
```

### With Debug Logging

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: run
      config-folder: ${CI_PROJECT_DIR}
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
      debug: "--debug"
```

### With Pinned Image Version

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: run
      image_tag: "1.0.0"
      config-folder: ${CI_PROJECT_DIR}
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
```

### With Custom Registry

If you mirror the airgapper image to an internal registry:

```yaml
include:
  - component: <airgapper-repo-group>/universal-airgapper@1
    inputs:
      stage: run
      image_registry: registry.internal.corp
      image_name: tools/universal-airgapper
      image_tag: "1.0.0"
      config-folder: ${CI_PROJECT_DIR}
      credentials-file: ${UNIVERSAL_AIRGAPPER_CREDS}
```

### Scheduled Pipeline

To run airgapper on a schedule, create a [pipeline schedule](https://docs.gitlab.com/ee/ci/pipelines/schedules.html) in your project and include the component as above. The job runs whenever the pipeline is triggered.

## Pipeline Build

The project's own `.gitlab-ci.yml` uses CI components for:

- **Semantic versioning**: automated version bumps via commit conventions.
- **Container builds**: Kaniko-based Docker builds on tags and branches.
    - On tags: publishes images tagged with the semver version and `latest`.
    - On branches: publishes images tagged with the commit SHA and branch name.
