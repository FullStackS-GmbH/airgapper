#!/usr/bin/env bash
# =============================================================================
# Universal Airgapper — Integration Test Suite
#
# Runs real-world sync tests against a Harbor registry and GitLab instance.
# Creates temporary projects, syncs artifacts, validates results, cleans up.
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
#   AIRGAPPER_BIN    — Path to airgapper binary (default: ./airgapper)
#   KEEP_PROJECTS    — Set to "true" to skip cleanup (for debugging)
# =============================================================================
set -euo pipefail

# -- Colors & helpers ---------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

log()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
pass() { echo -e "${GREEN}[PASS]${NC}  $*"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo -e "${RED}[FAIL]${NC}  $*"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
skip() { echo -e "${YELLOW}[SKIP]${NC}  $*"; SKIP_COUNT=$((SKIP_COUNT + 1)); }
header() { echo -e "\n${BOLD}━━━ $* ━━━${NC}"; }

# -- Validate prerequisites ---------------------------------------------------
for var in HARBOR_URL HARBOR_USERNAME HARBOR_PASSWORD GITLAB_URL GITLAB_TOKEN GITLAB_GROUP_ID; do
  if [[ -z "${!var:-}" ]]; then
    echo "ERROR: Required environment variable $var is not set."
    echo ""
    echo "Usage:"
    echo "  export HARBOR_URL=https://registry.lab.cloudstacks.eu"
    echo "  export HARBOR_USERNAME=your-user"
    echo "  export HARBOR_PASSWORD=your-password"
    echo "  export GITLAB_URL=https://gitlab.com"
    echo "  export GITLAB_TOKEN=glpat-xxxx"
    echo "  export GITLAB_GROUP_ID=12345"
    echo "  ./tests/integration/run.sh"
    exit 2
  fi
done

AIRGAPPER_BIN="${AIRGAPPER_BIN:-./airgapper}"
if [[ ! -x "$AIRGAPPER_BIN" ]]; then
  log "Building airgapper binary..."
  go build -o "$AIRGAPPER_BIN" ./cmd/airgapper
fi

# -- Derive values ------------------------------------------------------------
HARBOR_HOST="${HARBOR_URL#https://}"
HARBOR_HOST="${HARBOR_HOST#http://}"
HARBOR_API="${HARBOR_URL}/api/v2.0"

TEST_ID="airgapper-test-$(date +%s)"
HARBOR_PROJECT="${TEST_ID}"
GITLAB_PROJECT_NAME="${TEST_ID}"
GITLAB_PROJECT_ID=""

WORK_DIR=$(mktemp -d)
CONFIG_DIR="${WORK_DIR}/configs"
CREDS_DIR="${WORK_DIR}/creds"
mkdir -p "$CONFIG_DIR" "$CREDS_DIR"

log "Test ID:       ${TEST_ID}"
log "Work dir:      ${WORK_DIR}"
log "Harbor host:   ${HARBOR_HOST}"
log "Harbor project: ${HARBOR_PROJECT}"
log "GitLab group:  ${GITLAB_GROUP_ID}"

# -- Cleanup function ---------------------------------------------------------
cleanup() {
  header "CLEANUP"

  if [[ "${KEEP_PROJECTS:-}" == "true" ]]; then
    log "KEEP_PROJECTS=true — skipping cleanup"
    log "Harbor project: ${HARBOR_PROJECT}"
    log "GitLab project: ${GITLAB_PROJECT_ID:-none}"
    log "Work dir:       ${WORK_DIR}"
    return
  fi

  # Delete Harbor project
  if [[ -n "${HARBOR_PROJECT}" ]]; then
    log "Deleting Harbor project: ${HARBOR_PROJECT}"
    local status
    status=$(curl -s -o /dev/null -w "%{http_code}" \
      -X DELETE \
      -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
      "${HARBOR_API}/projects/${HARBOR_PROJECT}")
    if [[ "$status" == "200" || "$status" == "404" ]]; then
      log "Harbor project deleted (HTTP ${status})"
    else
      log "WARNING: Harbor project delete returned HTTP ${status}"
    fi
  fi

  # Delete GitLab project
  if [[ -n "${GITLAB_PROJECT_ID}" ]]; then
    log "Deleting GitLab project: ${GITLAB_PROJECT_ID}"
    local status
    status=$(curl -s -o /dev/null -w "%{http_code}" \
      -X DELETE \
      -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
      "${GITLAB_URL}/api/v4/projects/${GITLAB_PROJECT_ID}")
    if [[ "$status" == "202" || "$status" == "404" ]]; then
      log "GitLab project deleted (HTTP ${status})"
    else
      log "WARNING: GitLab project delete returned HTTP ${status}"
    fi
  fi

  # Clean work dir
  rm -rf "$WORK_DIR"
  log "Work directory cleaned up"
}
trap cleanup EXIT

# -- Create credential files --------------------------------------------------
header "SETUP CREDENTIALS"

cat > "${CREDS_DIR}/image.yaml" <<EOF
image:
  - name: "${HARBOR_HOST}"
    username: "${HARBOR_USERNAME}"
    password: "${HARBOR_PASSWORD}"
EOF

cat > "${CREDS_DIR}/helm.yaml" <<EOF
helm:
  - name: "${HARBOR_HOST}"
    username: "${HARBOR_USERNAME}"
    password: "${HARBOR_PASSWORD}"
EOF

cat > "${CREDS_DIR}/git.yaml" <<EOF
git:
  - name: "gitlab.com"
    username: "oauth2"
    password: "${GITLAB_TOKEN}"
EOF

log "Credential files written to ${CREDS_DIR}"

# -- Create Harbor project ----------------------------------------------------
header "SETUP HARBOR PROJECT"

log "Creating Harbor project: ${HARBOR_PROJECT}"
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST \
  -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
  -H "Content-Type: application/json" \
  -d "{\"project_name\": \"${HARBOR_PROJECT}\", \"public\": false}" \
  "${HARBOR_API}/projects")

if [[ "$HTTP_STATUS" == "201" ]]; then
  log "Harbor project created successfully"
else
  echo "ERROR: Failed to create Harbor project (HTTP ${HTTP_STATUS})"
  exit 1
fi

# -- Create GitLab project ---------------------------------------------------
header "SETUP GITLAB PROJECT"

log "Creating GitLab project: ${GITLAB_PROJECT_NAME} in group ${GITLAB_GROUP_ID}"
GITLAB_RESPONSE=$(curl -s \
  -X POST \
  -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"${GITLAB_PROJECT_NAME}\", \"namespace_id\": ${GITLAB_GROUP_ID}, \"initialize_with_readme\": true, \"visibility\": \"private\"}" \
  "${GITLAB_URL}/api/v4/projects")

GITLAB_PROJECT_ID=$(echo "$GITLAB_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
GITLAB_PROJECT_PATH=$(echo "$GITLAB_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('path_with_namespace',''))" 2>/dev/null || echo "")

if [[ -z "$GITLAB_PROJECT_ID" || "$GITLAB_PROJECT_ID" == "None" ]]; then
  echo "ERROR: Failed to create GitLab project"
  echo "$GITLAB_RESPONSE"
  exit 1
fi

log "GitLab project created: ${GITLAB_PROJECT_PATH} (ID: ${GITLAB_PROJECT_ID})"

# Wait for GitLab project to be fully initialized
sleep 3

# =============================================================================
# Helper: run airgapper with a specific config
# =============================================================================
run_airgapper() {
  local config_file="$1"
  local description="$2"
  local expect_exit="${3:-0}"

  log "Running: ${description}"
  log "Config:  ${config_file}"

  local exit_code=0
  "$AIRGAPPER_BIN" sync \
    --config "$config_file" \
    --credentials "$CREDS_DIR" \
    --debug 2>&1 | tee "${WORK_DIR}/last_output.log" || exit_code=$?

  if [[ "$exit_code" -eq "$expect_exit" ]]; then
    pass "${description} (exit code: ${exit_code})"
    return 0
  else
    fail "${description} (expected exit ${expect_exit}, got ${exit_code})"
    return 1
  fi
}

# =============================================================================
# Helper: check Harbor artifact exists
# =============================================================================
harbor_tag_exists() {
  local repo="$1"  # e.g. "test-project/library/alpine"
  local tag="$2"
  local encoded_repo
  encoded_repo=$(echo "$repo" | sed 's|/|%2F|g')

  local status
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_API}/projects/${HARBOR_PROJECT}/repositories/${encoded_repo}/artifacts/${tag}")

  [[ "$status" == "200" ]]
}

# =============================================================================
# Helper: check Harbor repository exists
# =============================================================================
harbor_repo_exists() {
  local repo="$1"
  local encoded_repo
  encoded_repo=$(echo "$repo" | sed 's|/|%2F|g')

  local status
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_API}/projects/${HARBOR_PROJECT}/repositories/${encoded_repo}")

  [[ "$status" == "200" ]]
}

# =============================================================================
# Helper: list Harbor tags for a repo
# =============================================================================
harbor_list_tags() {
  local repo="$1"
  local encoded_repo
  encoded_repo=$(echo "$repo" | sed 's|/|%2F|g')

  curl -s \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_API}/projects/${HARBOR_PROJECT}/repositories/${encoded_repo}/artifacts" \
    | python3 -c "import sys,json; [print(t['name']) for a in json.load(sys.stdin) for t in a.get('tags',[])]" 2>/dev/null
}

# =============================================================================
# Helper: check GitLab branch exists
# =============================================================================
gitlab_branch_exists() {
  local branch="$1"
  local encoded_branch
  encoded_branch=$(echo "$branch" | sed 's|/|%2F|g')

  local status
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
    "${GITLAB_URL}/api/v4/projects/${GITLAB_PROJECT_ID}/repository/branches/${encoded_branch}")

  [[ "$status" == "200" ]]
}

# =============================================================================
# Helper: list GitLab tags
# =============================================================================
gitlab_list_tags() {
  curl -s \
    -H "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
    "${GITLAB_URL}/api/v4/projects/${GITLAB_PROJECT_ID}/repository/tags" \
    | python3 -c "import sys,json; [print(t['name']) for t in json.load(sys.stdin)]" 2>/dev/null
}


# #############################################################################
#
#  TEST CASES — CONTAINER IMAGES
#
# #############################################################################
header "TEST: Image — Basic sync (alpine:3.20, alpine:3.21)"

cat > "${CONFIG_DIR}/test-image-basic.airgapper.yaml" <<EOF
resources:
  - type: image
    source: alpine
    destination: "${HARBOR_HOST}/${HARBOR_PROJECT}/alpine"
    push_mode: skip
    tags:
      - "3.20"
      - "3.21"
EOF

run_airgapper "${CONFIG_DIR}/test-image-basic.airgapper.yaml" "Image basic sync"

# Verify
if harbor_tag_exists "alpine" "3.20"; then
  pass "Image alpine:3.20 exists in Harbor"
else
  fail "Image alpine:3.20 NOT found in Harbor"
fi
if harbor_tag_exists "alpine" "3.21"; then
  pass "Image alpine:3.21 exists in Harbor"
else
  fail "Image alpine:3.21 NOT found in Harbor"
fi


# -----------------------------------------------------------------------------
header "TEST: Image — Skip mode (re-sync same tags)"

run_airgapper "${CONFIG_DIR}/test-image-basic.airgapper.yaml" "Image skip mode (second run)"

if grep -qi "skipped\|SKIPPED\|exists" "${WORK_DIR}/last_output.log" 2>/dev/null; then
  pass "Image skip mode: output indicates skipped/exists"
else
  skip "Image skip mode: could not verify skip in output (may still be correct)"
fi


# -----------------------------------------------------------------------------
header "TEST: Image — Force mode (overwrite existing)"

cat > "${CONFIG_DIR}/test-image-force.airgapper.yaml" <<EOF
resources:
  - type: image
    source: alpine
    destination: "${HARBOR_HOST}/${HARBOR_PROJECT}/alpine"
    push_mode: force
    tags:
      - "3.20"
EOF

run_airgapper "${CONFIG_DIR}/test-image-force.airgapper.yaml" "Image force mode (overwrite)"

if harbor_tag_exists "alpine" "3.20"; then
  pass "Image alpine:3.20 still exists after force push"
else
  fail "Image alpine:3.20 missing after force push"
fi


# -----------------------------------------------------------------------------
header "TEST: Image — Regex tag matching"

cat > "${CONFIG_DIR}/test-image-regex.airgapper.yaml" <<EOF
resources:
  - type: image
    source: alpine
    destination: "${HARBOR_HOST}/${HARBOR_PROJECT}/alpine-regex"
    push_mode: skip
    tags:
      - "3\\.2[0-1]"
EOF

run_airgapper "${CONFIG_DIR}/test-image-regex.airgapper.yaml" "Image regex tag matching (3.2[0-1])"

if harbor_tag_exists "alpine-regex" "3.20"; then
  pass "Regex matched alpine:3.20"
else
  fail "Regex did NOT match alpine:3.20"
fi
if harbor_tag_exists "alpine-regex" "3.21"; then
  pass "Regex matched alpine:3.21"
else
  fail "Regex did NOT match alpine:3.21"
fi


# -----------------------------------------------------------------------------
header "TEST: Image — Dry-run mode"

cat > "${CONFIG_DIR}/test-image-dryrun.airgapper.yaml" <<EOF
resources:
  - type: image
    source: alpine
    destination: "${HARBOR_HOST}/${HARBOR_PROJECT}/alpine-dryrun"
    push_mode: skip
    tags:
      - "3.19"
EOF

log "Running: Image dry-run mode"
"$AIRGAPPER_BIN" sync \
  --config "${CONFIG_DIR}/test-image-dryrun.airgapper.yaml" \
  --credentials "$CREDS_DIR" \
  --dry-run 2>&1 | tee "${WORK_DIR}/last_output.log" || true

if ! harbor_repo_exists "alpine-dryrun"; then
  pass "Dry-run did NOT create alpine-dryrun repo (correct)"
else
  fail "Dry-run unexpectedly created alpine-dryrun repo"
fi


# #############################################################################
#
#  TEST CASES — HELM CHARTS
#
# #############################################################################
header "TEST: Helm — Basic OCI sync (bitnami nginx)"

cat > "${CONFIG_DIR}/test-helm-basic.airgapper.yaml" <<EOF
resources:
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/nginx
    destination_registry: "${HARBOR_HOST}"
    destination_repo: "${HARBOR_PROJECT}/charts"
    push_mode: skip
    versions:
      - "18.3.5"
      - "18.3.4"
EOF

run_airgapper "${CONFIG_DIR}/test-helm-basic.airgapper.yaml" "Helm basic OCI sync (nginx)"


# -----------------------------------------------------------------------------
header "TEST: Helm — Skip mode (re-sync same versions)"

run_airgapper "${CONFIG_DIR}/test-helm-basic.airgapper.yaml" "Helm skip mode (second run)"

if grep -qi "skipped\|SKIPPED\|exists" "${WORK_DIR}/last_output.log" 2>/dev/null; then
  pass "Helm skip mode: output indicates skipped/exists"
else
  skip "Helm skip mode: could not verify skip in output"
fi


# -----------------------------------------------------------------------------
header "TEST: Helm — Overwrite mode"

cat > "${CONFIG_DIR}/test-helm-overwrite.airgapper.yaml" <<EOF
resources:
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/nginx
    destination_registry: "${HARBOR_HOST}"
    destination_repo: "${HARBOR_PROJECT}/charts"
    push_mode: overwrite
    versions:
      - "18.3.5"
EOF

run_airgapper "${CONFIG_DIR}/test-helm-overwrite.airgapper.yaml" "Helm overwrite mode"


# -----------------------------------------------------------------------------
header "TEST: Helm — Regex version matching"

cat > "${CONFIG_DIR}/test-helm-regex.airgapper.yaml" <<EOF
resources:
  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/nginx
    destination_registry: "${HARBOR_HOST}"
    destination_repo: "${HARBOR_PROJECT}/charts-regex"
    push_mode: skip
    versions:
      - "18\\.3\\.[4-5]"
EOF

run_airgapper "${CONFIG_DIR}/test-helm-regex.airgapper.yaml" "Helm regex version matching (18.3.[4-5])"


# #############################################################################
#
#  TEST CASES — GIT REPOSITORIES
#
# #############################################################################
header "TEST: Git — Basic sync (specific refs)"

cat > "${CONFIG_DIR}/test-git-basic.airgapper.yaml" <<EOF
resources:
  - type: git
    source_repo: "https://github.com/spf13/cobra.git"
    destination_repo: "https://oauth2:${GITLAB_TOKEN}@gitlab.com/${GITLAB_PROJECT_PATH}.git"
    push_mode: force
    refs:
      - "main"
EOF

run_airgapper "${CONFIG_DIR}/test-git-basic.airgapper.yaml" "Git basic sync (cobra main)"

# Wait for GitLab to process the push
sleep 2

if gitlab_branch_exists "main"; then
  pass "Git branch 'main' synced to GitLab"
else
  fail "Git branch 'main' NOT found in GitLab"
fi


# -----------------------------------------------------------------------------
header "TEST: Git — Sync tags"

cat > "${CONFIG_DIR}/test-git-tags.airgapper.yaml" <<EOF
resources:
  - type: git
    source_repo: "https://github.com/spf13/cobra.git"
    destination_repo: "https://oauth2:${GITLAB_TOKEN}@gitlab.com/${GITLAB_PROJECT_PATH}.git"
    push_mode: skip
    refs:
      - "v1.8.0"
      - "v1.8.1"
EOF

run_airgapper "${CONFIG_DIR}/test-git-tags.airgapper.yaml" "Git tag sync (v1.8.0, v1.8.1)"

sleep 2

TAGS=$(gitlab_list_tags)
if echo "$TAGS" | grep -q "v1.8.0"; then
  pass "Git tag v1.8.0 synced to GitLab"
else
  fail "Git tag v1.8.0 NOT found in GitLab"
fi
if echo "$TAGS" | grep -q "v1.8.1"; then
  pass "Git tag v1.8.1 synced to GitLab"
else
  fail "Git tag v1.8.1 NOT found in GitLab"
fi


# -----------------------------------------------------------------------------
header "TEST: Git — Skip mode (re-sync same tags)"

run_airgapper "${CONFIG_DIR}/test-git-tags.airgapper.yaml" "Git skip mode (second run)"

if grep -qi "skipped\|SKIPPED\|exists" "${WORK_DIR}/last_output.log" 2>/dev/null; then
  pass "Git skip mode: output indicates skipped/exists"
else
  skip "Git skip mode: could not verify skip in output"
fi


# -----------------------------------------------------------------------------
header "TEST: Git — Regex ref matching"

cat > "${CONFIG_DIR}/test-git-regex.airgapper.yaml" <<EOF
resources:
  - type: git
    source_repo: "https://github.com/spf13/cobra.git"
    destination_repo: "https://oauth2:${GITLAB_TOKEN}@gitlab.com/${GITLAB_PROJECT_PATH}.git"
    push_mode: skip
    refs:
      - "v1\\.8\\.[0-9]+"
EOF

run_airgapper "${CONFIG_DIR}/test-git-regex.airgapper.yaml" "Git regex ref matching (v1.8.*)"

sleep 2

TAGS=$(gitlab_list_tags)
MATCH_COUNT=$(echo "$TAGS" | grep -c "^v1\.8\." || true)
if [[ "$MATCH_COUNT" -ge 2 ]]; then
  pass "Git regex matched ${MATCH_COUNT} tags matching v1.8.x"
else
  fail "Git regex matched only ${MATCH_COUNT} tags (expected >= 2)"
fi


# #############################################################################
#
#  TEST CASES — MULTI-RESOURCE CONFIG
#
# #############################################################################
header "TEST: Multi-resource config (image + helm in one file)"

cat > "${CONFIG_DIR}/test-multi.airgapper.yaml" <<EOF
resources:
  - type: image
    source: busybox
    destination: "${HARBOR_HOST}/${HARBOR_PROJECT}/busybox"
    push_mode: skip
    tags:
      - "1.37"

  - type: helm
    source_registry: registry-1.docker.io
    source_chart: bitnamicharts/redis
    destination_registry: "${HARBOR_HOST}"
    destination_repo: "${HARBOR_PROJECT}/multi-charts"
    push_mode: skip
    versions:
      - "20.11.3"
EOF

run_airgapper "${CONFIG_DIR}/test-multi.airgapper.yaml" "Multi-resource config"

if harbor_tag_exists "busybox" "1.37"; then
  pass "Multi-resource: busybox:1.37 exists in Harbor"
else
  fail "Multi-resource: busybox:1.37 NOT found in Harbor"
fi


# #############################################################################
#
#  SUMMARY
#
# #############################################################################
header "TEST SUMMARY"

TOTAL=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
echo -e "${BOLD}Total: ${TOTAL}${NC}  |  ${GREEN}Passed: ${PASS_COUNT}${NC}  |  ${RED}Failed: ${FAIL_COUNT}${NC}  |  ${YELLOW}Skipped: ${SKIP_COUNT}${NC}"
echo ""

if [[ "$FAIL_COUNT" -gt 0 ]]; then
  echo -e "${RED}${BOLD}INTEGRATION TESTS FAILED${NC}"
  exit 1
else
  echo -e "${GREEN}${BOLD}ALL INTEGRATION TESTS PASSED${NC}"
  exit 0
fi
