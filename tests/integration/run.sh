#!/usr/bin/env bash
# =============================================================================
# Universal Airgapper — Integration Infrastructure Manager
#
# Creates and deletes Harbor projects and GitLab projects used for manual
# integration testing. Run "setup" before testing and "cleanup" after.
#
# Usage:
#   ./tests/integration/run.sh setup     # create Harbor project + GitLab project
#   ./tests/integration/run.sh cleanup   # delete them
#
# Required environment variables:
#   HARBOR_URL       — Harbor base URL (e.g. https://registry.lab.cloudstacks.eu)
#   HARBOR_USERNAME  — Harbor username
#   HARBOR_PASSWORD  — Harbor password (or robot token)
#   GITLAB_URL       — GitLab base URL (e.g. https://gitlab.com)
#   GITLAB_TOKEN     — GitLab personal access token (api scope)
#   GITLAB_GROUP_ID  — GitLab group ID (numeric) to create the test project in
#
# Optional:
#   HARBOR_PROJECT   — Harbor project name (default: ddrack)
#   GITLAB_PROJECT   — GitLab project name (default: airgapper-juice-shop)
#   STATE_FILE       — Path to persist project IDs between setup/cleanup
# =============================================================================
set -euo pipefail

# -- Colors & helpers ---------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()    { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()     { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()   { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()    { echo -e "${RED}[ERROR]${NC} $*"; }
header() { echo -e "\n${BOLD}━━━ $* ━━━${NC}"; }

# -- Validate prerequisites ---------------------------------------------------
for var in HARBOR_URL HARBOR_USERNAME HARBOR_PASSWORD GITLAB_URL GITLAB_TOKEN GITLAB_GROUP_ID; do
  if [[ -z "${!var:-}" ]]; then
    err "Required environment variable $var is not set."
    echo ""
    echo "Usage:"
    echo "  export HARBOR_URL=https://registry.lab.cloudstacks.eu"
    echo "  export HARBOR_USERNAME=your-user"
    echo "  export HARBOR_PASSWORD=your-password"
    echo "  export GITLAB_URL=https://gitlab.com"
    echo "  export GITLAB_TOKEN=glpat-xxxx"
    echo "  export GITLAB_GROUP_ID=12345"
    echo ""
    echo "  ./tests/integration/run.sh setup"
    echo "  ./tests/integration/run.sh cleanup"
    exit 2
  fi
done

# -- Defaults -----------------------------------------------------------------
HARBOR_HOST="${HARBOR_URL#https://}"
HARBOR_HOST="${HARBOR_HOST#http://}"
HARBOR_API="${HARBOR_URL}/api/v2.0"

HARBOR_PROJECT="${HARBOR_PROJECT:-ddrack}"
GITLAB_PROJECT="${GITLAB_PROJECT:-airgapper-juice-shop}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${STATE_FILE:-${SCRIPT_DIR}/.state.env}"
CONFIG_DIR="${SCRIPT_DIR}/configs"
CREDS_DIR="${SCRIPT_DIR}/creds"

# =============================================================================
# setup — create Harbor project and GitLab project
# =============================================================================
do_setup() {
  header "SETUP"

  # -- Create Harbor project --------------------------------------------------
  header "Harbor Project: ${HARBOR_PROJECT}"

  # Check if project already exists
  HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_API}/projects/${HARBOR_PROJECT}")

  if [[ "$HTTP_STATUS" == "200" ]]; then
    ok "Harbor project '${HARBOR_PROJECT}' already exists — reusing"
  else
    log "Creating Harbor project: ${HARBOR_PROJECT}"
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
      -X POST \
      -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
      -H "Content-Type: application/json" \
      -d "{\"project_name\": \"${HARBOR_PROJECT}\", \"public\": false}" \
      "${HARBOR_API}/projects")

    if [[ "$HTTP_STATUS" == "201" ]]; then
      ok "Harbor project '${HARBOR_PROJECT}' created"
    else
      err "Failed to create Harbor project (HTTP ${HTTP_STATUS})"
      exit 1
    fi
  fi

  # -- Create GitLab project -------------------------------------------------
  header "GitLab Project: ${GITLAB_PROJECT}"

  # Check if project already exists in the group
  EXISTING=$(curl -s \
    -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
    "${GITLAB_URL}/api/v4/groups/${GITLAB_GROUP_ID}/projects?search=${GITLAB_PROJECT}" \
    | python3 -c "
import sys, json
projects = json.load(sys.stdin)
for p in projects:
    if p['name'] == '${GITLAB_PROJECT}':
        print(p['id'])
        break
" 2>/dev/null || echo "")

  if [[ -n "$EXISTING" ]]; then
    GITLAB_PROJECT_ID="$EXISTING"
    GITLAB_PROJECT_PATH=$(curl -s \
      -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      "${GITLAB_URL}/api/v4/projects/${GITLAB_PROJECT_ID}" \
      | python3 -c "import sys,json; print(json.load(sys.stdin)['path_with_namespace'])" 2>/dev/null)
    ok "GitLab project '${GITLAB_PROJECT}' already exists (ID: ${GITLAB_PROJECT_ID}) — reusing"
  else
    log "Creating GitLab project: ${GITLAB_PROJECT} in group ${GITLAB_GROUP_ID}"
    GITLAB_RESPONSE=$(curl -s \
      -X POST \
      -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "{\"name\": \"${GITLAB_PROJECT}\", \"namespace_id\": ${GITLAB_GROUP_ID}, \"initialize_with_readme\": true, \"visibility\": \"private\"}" \
      "${GITLAB_URL}/api/v4/projects")

    GITLAB_PROJECT_ID=$(echo "$GITLAB_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    GITLAB_PROJECT_PATH=$(echo "$GITLAB_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('path_with_namespace',''))" 2>/dev/null || echo "")

    if [[ -z "$GITLAB_PROJECT_ID" || "$GITLAB_PROJECT_ID" == "None" ]]; then
      err "Failed to create GitLab project"
      echo "$GITLAB_RESPONSE"
      exit 1
    fi

    ok "GitLab project created: ${GITLAB_PROJECT_PATH} (ID: ${GITLAB_PROJECT_ID})"
  fi

  # -- Write state file -------------------------------------------------------
  cat > "$STATE_FILE" <<EOF
HARBOR_PROJECT=${HARBOR_PROJECT}
GITLAB_PROJECT_ID=${GITLAB_PROJECT_ID}
GITLAB_PROJECT_PATH=${GITLAB_PROJECT_PATH}
EOF

  # -- Write rendered credential files ----------------------------------------
  header "Credential Files"

  cat > "${CREDS_DIR}/image.creds.airgapper.yaml" <<EOF
image:
  - name: "${HARBOR_HOST}"
    username: "${HARBOR_USERNAME}"
    password: "${HARBOR_PASSWORD}"
EOF

  cat > "${CREDS_DIR}/helm.creds.airgapper.yaml" <<EOF
helm:
  - name: "${HARBOR_HOST}"
    username: "${HARBOR_USERNAME}"
    password: "${HARBOR_PASSWORD}"
EOF

  cat > "${CREDS_DIR}/git.creds.airgapper.yaml" <<EOF
git:
  - name: "gitlab.com"
    username: "oauth2"
    password: "${GITLAB_TOKEN}"
EOF

  ok "Credential files written to ${CREDS_DIR}/"

  # -- Write rendered git config with actual project path ---------------------
  header "Config Files"

  cat > "${CONFIG_DIR}/git.config.airgapper.yaml" <<EOF
resources:
  # ---------------------------------------------------------------------------
  # OWASP Juice Shop — GitHub → private GitLab group
  # ---------------------------------------------------------------------------
  - type: git
    source_repo: "https://github.com/juice-shop/juice-shop.git"
    destination_repo: "https://oauth2:${GITLAB_TOKEN}@gitlab.com/${GITLAB_PROJECT_PATH}.git"
    push_mode: force
    refs:
      - "main"
      - "v[0-9]+\\\\.[0-9]+\\\\.[0-9]+"   # regex: all semver tags (e.g. v17.1.1)
EOF

  ok "Git config rendered with project path: ${GITLAB_PROJECT_PATH}"

  # -- Summary ----------------------------------------------------------------
  header "SETUP COMPLETE"
  log ""
  log "Harbor project:  ${HARBOR_PROJECT}"
  log "Harbor registry: ${HARBOR_HOST}/${HARBOR_PROJECT}"
  log "GitLab project:  ${GITLAB_PROJECT_PATH} (ID: ${GITLAB_PROJECT_ID})"
  log "State file:      ${STATE_FILE}"
  log ""
  log "Config dir:      ${CONFIG_DIR}/"
  log "Creds dir:       ${CREDS_DIR}/"
  log ""
  log "Now run the airgapper manually:"
  log ""
  echo -e "  ${BOLD}./airgapper sync --config ${CONFIG_DIR}/ --credentials ${CREDS_DIR}/ --debug${NC}"
  log ""
  log "When done, clean up with:"
  log ""
  echo -e "  ${BOLD}./tests/integration/run.sh cleanup${NC}"
  log ""
}

# =============================================================================
# cleanup — delete Harbor project and GitLab project
# =============================================================================
do_cleanup() {
  header "CLEANUP"

  # Load state file if it exists
  if [[ -f "$STATE_FILE" ]]; then
    log "Loading state from ${STATE_FILE}"
    # shellcheck disable=SC1090
    source "$STATE_FILE"
  fi

  GITLAB_PROJECT_ID="${GITLAB_PROJECT_ID:-}"

  # If we still don't have a GitLab project ID, try to find it
  if [[ -z "$GITLAB_PROJECT_ID" ]]; then
    log "No state file found — searching for GitLab project '${GITLAB_PROJECT}' in group ${GITLAB_GROUP_ID}"
    GITLAB_PROJECT_ID=$(curl -s \
      -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      "${GITLAB_URL}/api/v4/groups/${GITLAB_GROUP_ID}/projects?search=${GITLAB_PROJECT}" \
      | python3 -c "
import sys, json
projects = json.load(sys.stdin)
for p in projects:
    if p['name'] == '${GITLAB_PROJECT}':
        print(p['id'])
        break
" 2>/dev/null || echo "")
  fi

  # -- Delete Harbor project --------------------------------------------------
  log "Deleting Harbor project: ${HARBOR_PROJECT}"
  HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -X DELETE \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_API}/projects/${HARBOR_PROJECT}")

  if [[ "$HTTP_STATUS" == "200" ]]; then
    ok "Harbor project '${HARBOR_PROJECT}' deleted"
  elif [[ "$HTTP_STATUS" == "404" ]]; then
    warn "Harbor project '${HARBOR_PROJECT}' not found (already deleted?)"
  else
    warn "Harbor project delete returned HTTP ${HTTP_STATUS}"
  fi

  # -- Delete GitLab project --------------------------------------------------
  if [[ -n "$GITLAB_PROJECT_ID" ]]; then
    log "Deleting GitLab project: ${GITLAB_PROJECT_ID}"
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
      -X DELETE \
      -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      "${GITLAB_URL}/api/v4/projects/${GITLAB_PROJECT_ID}")

    if [[ "$HTTP_STATUS" == "202" ]]; then
      ok "GitLab project ${GITLAB_PROJECT_ID} deleted"
    elif [[ "$HTTP_STATUS" == "404" ]]; then
      warn "GitLab project ${GITLAB_PROJECT_ID} not found (already deleted?)"
    else
      warn "GitLab project delete returned HTTP ${HTTP_STATUS}"
    fi
  else
    warn "No GitLab project ID found — skipping GitLab cleanup"
  fi

  # -- Remove state file ------------------------------------------------------
  if [[ -f "$STATE_FILE" ]]; then
    rm -f "$STATE_FILE"
    log "State file removed"
  fi

  header "CLEANUP COMPLETE"
}

# =============================================================================
# Main
# =============================================================================
ACTION="${1:-}"

case "$ACTION" in
  setup)
    do_setup
    ;;
  cleanup)
    do_cleanup
    ;;
  *)
    echo "Usage: $0 {setup|cleanup}"
    echo ""
    echo "  setup    Create Harbor project and GitLab project for testing"
    echo "  cleanup  Delete the created Harbor project and GitLab project"
    exit 2
    ;;
esac
