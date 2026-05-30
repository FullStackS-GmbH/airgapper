#!/usr/bin/env bash
# E2E test for `airgapper helm images`.
# Pulls real public Helm charts (no auth required) and validates the output YAML.
#
# Usage:
#   ./tests/integration/helm-images.sh
#
# Requires: network access, Go toolchain
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
BINARY="${ROOT_DIR}/airgapper-e2e-bin"
CONFIG="${SCRIPT_DIR}/configs/helm.config.airgapper.yaml"
CREDS_FILE="$(mktemp /tmp/helm-images-test.XXXXXX.creds.airgapper.yaml)"
OUTPUT_FILE="$(mktemp /tmp/helm-images-test.XXXXXX.yaml)"
TARGET_CRED="registry.test.local"

GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

pass() { echo -e "${GREEN}[PASS]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
log()  { echo -e "${CYAN}[INFO]${NC} $*"; }

cleanup() { rm -f "${BINARY}" "${CREDS_FILE}" "${OUTPUT_FILE}"; }
trap cleanup EXIT

# ── Build ─────────────────────────────────────────────────────────────────────
log "Building binary..."
go build -o "${BINARY}" "${ROOT_DIR}/cmd/airgapper"
pass "Binary built"

# ── Credentials ───────────────────────────────────────────────────────────────
cat > "${CREDS_FILE}" <<EOF
helm:
  - name: "${TARGET_CRED}"
    username: ""
    password: ""
EOF

# ── Run ───────────────────────────────────────────────────────────────────────
log "Running helm images (pulling public charts — may take ~30s)..."
"${BINARY}" helm images \
    --config  "${CONFIG}" \
    --credentials "${CREDS_FILE}" \
    --target-credentials-ref "${TARGET_CRED}" \
    --output  "${OUTPUT_FILE}"

# ── Assertions ────────────────────────────────────────────────────────────────
[[ -s "${OUTPUT_FILE}" ]] \
    || fail "Output file is empty or missing"
pass "Output file created"

grep -q "^resources:" "${OUTPUT_FILE}" \
    || fail "Missing 'resources:' key"
pass "Has 'resources:' key"

grep -q "type: image" "${OUTPUT_FILE}" \
    || fail "No 'type: image' entries"
pass "Has 'type: image' entries"

grep -q "target_credentials_ref: ${TARGET_CRED}" "${OUTPUT_FILE}" \
    || fail "Wrong or missing target_credentials_ref"
pass "target_credentials_ref correct"

grep -q "destination: ${TARGET_CRED}/" "${OUTPUT_FILE}" \
    || fail "Destinations not prefixed with '${TARGET_CRED}/'"
pass "Destination prefix correct"

grep -q "# from helm:" "${OUTPUT_FILE}" \
    || fail "Missing inline '# from helm:' comments"
pass "Inline chart-source comments present"

# Bitnami nginx chart (OCI, public) must produce image entries.
grep -q "bitnami/nginx" "${OUTPUT_FILE}" \
    || fail "Expected bitnami/nginx images not found — chart pull likely failed"
pass "bitnami/nginx images present"

# Bitnami redis/valkey chart (OCI, public) must produce image entries.
grep -qE "bitnami/(redis|valkey)" "${OUTPUT_FILE}" \
    || fail "Expected bitnami/redis or bitnami/valkey images not found"
pass "bitnami redis-family images present"

# Tags must carry double-quoted strings.
grep -qE '"[0-9]+\.[0-9]' "${OUTPUT_FILE}" \
    || fail "No quoted version tags found"
pass "Version tags are double-quoted"

# ── Preview ───────────────────────────────────────────────────────────────────
echo ""
log "Output (first 50 lines):"
head -50 "${OUTPUT_FILE}"
echo ""
pass "All assertions passed"
