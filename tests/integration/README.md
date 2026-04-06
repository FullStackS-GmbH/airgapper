# Integration Tests

Real-world integration tests that sync artifacts against live Harbor and GitLab instances.

## Prerequisites

- A Harbor registry with API access and permission to create/delete projects
- A GitLab instance with API access and permission to create/delete projects in a group
- The `airgapper` binary (built automatically if not present)
- `curl`, `python3` (for JSON parsing)

## Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `HARBOR_URL` | Harbor base URL | `https://registry.lab.cloudstacks.eu` |
| `HARBOR_USERNAME` | Harbor username | `user@example.com` |
| `HARBOR_PASSWORD` | Harbor password or robot token | `secret` |
| `GITLAB_URL` | GitLab base URL | `https://gitlab.com` |
| `GITLAB_TOKEN` | GitLab personal access token (api scope) | `glpat-xxxx` |
| `GITLAB_GROUP_ID` | Numeric ID of the GitLab group for test projects | `12345` |

## Optional Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AIRGAPPER_BIN` | Path to airgapper binary | `./airgapper` |
| `KEEP_PROJECTS` | Set to `true` to skip cleanup (debugging) | `false` |

## Running

```bash
# Set credentials
export HARBOR_URL=https://registry.lab.cloudstacks.eu
export HARBOR_USERNAME=your-user
export HARBOR_PASSWORD=your-password
export GITLAB_URL=https://gitlab.com
export GITLAB_TOKEN=glpat-xxxx
export GITLAB_GROUP_ID=12345

# Run
./tests/integration/run.sh
```

## Test Cases

### Container Images
| Test | Description |
|------|-------------|
| Basic sync | Sync `alpine:3.20` and `alpine:3.21` from Docker Hub to Harbor |
| Skip mode | Re-run same sync — second run should skip existing tags |
| Force mode | Sync with `push_mode: force` — overwrites existing tags |
| Regex tags | Pattern `3\.2[0-1]` matches both 3.20 and 3.21 |
| Dry-run | `--dry-run` flag prevents any writes to Harbor |

### Helm Charts
| Test | Description |
|------|-------------|
| Basic OCI sync | Sync `bitnami/nginx` chart versions to Harbor OCI registry |
| Skip mode | Re-run same sync — second run should skip existing versions |
| Overwrite mode | Sync with `push_mode: overwrite` — re-pushes existing |
| Regex versions | Pattern `18\.3\.[4-5]` matches 18.3.4 and 18.3.5 |

### Git Repositories
| Test | Description |
|------|-------------|
| Basic sync | Sync `main` branch from `spf13/cobra` to GitLab |
| Tag sync | Sync specific tags (`v1.8.0`, `v1.8.1`) |
| Skip mode | Re-run same sync — second run should skip existing refs |
| Regex refs | Pattern `v1\.8\.[0-9]+` matches all v1.8.x tags |

### Multi-Resource
| Test | Description |
|------|-------------|
| Combined config | Single config file with both image and helm resources |

## Cleanup

The script automatically deletes the test Harbor project and GitLab project on exit (even on failure). Set `KEEP_PROJECTS=true` to preserve them for debugging.
