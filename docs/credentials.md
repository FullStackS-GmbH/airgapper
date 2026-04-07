# Credential Management

This document covers the credential file format, resolution logic, and security recommendations for Universal Airgapper.

## Providing Credentials

Credentials are supplied via the `--credentials` flag (or `AIRGAPPER_CREDENTIALS` env var). The value can be:

- A **single YAML file**: `--credentials creds.yaml`
- A **folder**: `--credentials ./creds/`

When a folder is provided, all files matching `*.creds.airgapper.yaml` or `*.creds.airgapper.yml` are discovered and merged.
Credentials are optional, if omitted, anonymous access is used for all operations.

## File Format

Credentials are organized by type: `image`, `helm`, and `git`.

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
    username: AzureReposAuthnSucks
    password: REPO_AUTH_HEADER
```

### Fields by Type

#### Image Credentials

| Field      | Required | Description                                                 |
|------------|----------|-------------------------------------------------------------|
| `name`     | yes      | Unique identifier (used for reference or hostname matching) |
| `username` | yes      | Registry username                                           |
| `password` | yes      | Registry password or token                                  |

#### Helm Credentials

| Field      | Required | Description                |
|------------|----------|----------------------------|
| `name`     | yes      | Unique identifier          |
| `username` | yes      | Registry username          |
| `password` | yes      | Registry password or token |

#### Git Credentials

| Field          | Required | Description                                            |
|----------------|----------|--------------------------------------------------------|
| `name`         | yes      | Unique identifier                                      |
| `username`     | no       | Git username (for HTTPS auth)                          |
| `password`     | no       | Git password or personal access token (for HTTPS auth) |
| `ssh_key_path` | no       | Absolute path to SSH private key file                  |

For Git, provide either `username`/`password` (HTTPS) or `ssh_key_path` (SSH), depending on the repository URL protocol.

## Credential Resolution

Resources can reference credentials explicitly, or the tool resolves them automatically by hostname.

### Resolution Order

1. **By explicit reference**: If the resource specifies `source_credentials_ref` or `target_credentials_ref`, the tool looks up the credential by its `name` field. If the referenced name does not exist, the operation fails with an error.

2. **By hostname**: If no explicit reference is set, the tool extracts the hostname from the endpoint URL and searches for a credential whose `name` matches that hostname.

3. **Anonymous access**: If no credential is found by either method, the operation proceeds without authentication.

### Example

Given this credential file:

```yaml
image:
  - name: registry.example.com
    username: user
    password: pass
  - name: special-reg
    username: admin
    password: token
```

And this resource config:

```yaml
resources:
  # Uses credential "special-reg" by explicit reference
  - type: image
    source: registry.example.com/team/app
    destination: internal.corp/mirror/app
    source_credentials_ref: special-reg
    tags: [ "v1.0" ]

  # Uses credential "registry.example.com" by hostname match
  - type: image
    source: registry.example.com/other/app
    destination: internal.corp/mirror/other
    tags: [ "latest" ]
```

## Azure Repos Workaround

Azure Repos HTTPS authentication requires a special approach because it does not support standard Git credential helpers for all operations.

To authenticate with Azure Repos via HTTPS:

1. Set the `username` to `AzureReposAuthnSucks`.
2. Set the `password` to the **name of an environment variable** that contains the authorization header.

### Setup

```shell
# Create the auth header env var
export REPO_AUTH_HEADER=$(echo -n "Authorization: Basic "$(printf ":%s" "$MY_PAT" | base64))
```

### Credential Config

```yaml
git:
  - name: azure-repos
    username: AzureReposAuthnSucks
    password: REPO_AUTH_HEADER    # Name of the env var, NOT the actual header
```

Under the hood, the git transporter reads the environment variable specified in `password` and passes it as an HTTP extra header to Git.

> **Note**: This workaround is only supported for pulling from Azure Repos. See the [Azure DevOps authentication docs](https://learn.microsoft.com/en-us/azure/devops/repos/git/auth-overview) for background.

## SSH Key Configuration

For Git operations over SSH, you need a valid SSH configuration. This includes:

- An SSH private key file
- A `known_hosts` file (to avoid host verification prompts)
- Optionally, an SSH config file mapping hosts to keys

### Directory Structure

```
.ssh/
  config          # SSH config mapping hosts to identity files
  id_ed25519      # Private key
  known_hosts     # Known host fingerprints
```

### SSH Config Example

```
Host github.com
    IdentityFile ~/.ssh/id_ed25519

Host gitlab.com
    IdentityFile ~/.ssh/id_ed25519
```

### Generating known_hosts

```shell
ssh-keyscan github.com gitlab.com >> ~/.ssh/known_hosts
```

### Running in a Container

When using the container image, mount your SSH directory:

```shell
docker run --rm \
  -v $(pwd)/.ssh:/home/nonroot/.ssh:ro \
  -v $(pwd)/configs:/mnt/configs \
  ghcr.io/fullstacks-gmbh/universal-airgapper:latest \
  sync --config /mnt/configs/
```

In Kubernetes, create a Secret containing the SSH key and known_hosts, and mount it into the pod.

## Multiple Credential Files

For separation of concerns, use a folder with multiple credential files:

```
creds/
  image.creds.airgapper.yaml   # image credentials
  helm.creds.airgapper.yaml    # helm credentials
  git.creds.airgapper.yaml     # git credentials
```

```shell
airgapper sync --config ./configs/ --credentials ./creds/
```

All files are loaded and their credential lists are merged.

## Security Recommendations

- **File permissions**: Set credential files to `600` (owner read/write only).
  ```shell
  chmod 600 creds/*.yaml
  ```
- **Never commit credentials** to version control. Add credential paths to `.gitignore`.
- **Rotate credentials** regularly, especially personal access tokens.
- **Use separate credentials** for different environments (dev, staging, production).
- **Prefer SSH keys** over passwords for Git operations.
- **Use short-lived tokens** where possible (e.g., CI/CD pipeline tokens).
- **The tool never logs credential values**, even at DEBUG level. Only credential names and file paths appear in logs.
