#!/bin/sh

set -eu

REPO_OWNER="FullStackS-GmbH"
REPO_NAME="airgapper"
BINARY_NAME="airgapper"
API_BASE_URL="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"
COSIGN_KEY_URL="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/cosign.pub"

COLOR_RESET=""
COLOR_INFO=""
COLOR_ERROR=""
COLOR_PROMPT=""

init_colors() {
  if [ -n "${NO_COLOR:-}" ]; then
    return
  fi

  if [ "${TERM:-}" = "dumb" ]; then
    return
  fi

  if [ -t 1 ] || [ -t 2 ]; then
    esc="$(printf '\033')"
    COLOR_RESET="${esc}[0m"
    COLOR_INFO="${esc}[36m"
    COLOR_ERROR="${esc}[31m"
    COLOR_PROMPT="${esc}[33m"
  fi
}

fatal() {
  printf '%s[airgapper-install] ERROR:%s %s\n' "$COLOR_ERROR" "$COLOR_RESET" "$*" >&2
  exit 1
}

is_true() {
  value=$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')
  case "$value" in
    1|true|yes|y|on)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

QUIET_VALUE="${AIRGAPPER_QUIET:-0}"
ASSUME_YES_VALUE="${AIRGAPPER_YES:-0}"

log() {
  if ! is_true "$QUIET_VALUE"; then
    printf '%s[airgapper-install]%s %s\n' "$COLOR_INFO" "$COLOR_RESET" "$*"
  fi
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fatal "required command '$1' is not available"
}

expand_path() {
  input_path="$1"
  # Tildes are quoted on purpose here: they are literal patterns to match, not
  # paths to expand.
  # shellcheck disable=SC2088
  case "$input_path" in
    "~")
      printf '%s' "$HOME"
      ;;
    "~/"*)
      printf '%s/%s' "$HOME" "${input_path#"~/"}"
      ;;
    *)
      printf '%s' "$input_path"
      ;;
  esac
}

path_contains() {
  directory="$1"
  case ":$PATH:" in
    *":$directory:"*|*":$directory/:"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

normalize_os() {
  raw_os=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$raw_os" in
    linux)
      printf 'linux'
      ;;
    darwin|mac|macos|osx)
      printf 'darwin'
      ;;
    windows|win)
      printf 'windows'
      ;;
    *)
      printf ''
      ;;
  esac
}

normalize_arch() {
  raw_arch=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$raw_arch" in
    x86_64|x64|amd64)
      printf 'amd64'
      ;;
    aarch64|arm64)
      printf 'arm64'
      ;;
    *)
      printf ''
      ;;
  esac
}

resolve_os() {
  if [ -n "${AIRGAPPER_OS:-}" ]; then
    os_value=$(normalize_os "$AIRGAPPER_OS")
    [ -n "$os_value" ] || fatal "unsupported AIRGAPPER_OS='$AIRGAPPER_OS'"
    printf '%s' "$os_value"
    return 0
  fi

  detected=$(normalize_os "$(uname -s 2>/dev/null || true)")
  if [ -n "$detected" ]; then
    printf '%s' "$detected"
    return 0
  fi

  printf 'linux'
}

resolve_arch() {
  if [ -n "${AIRGAPPER_ARCH:-}" ]; then
    arch_value=$(normalize_arch "$AIRGAPPER_ARCH")
    [ -n "$arch_value" ] || fatal "unsupported AIRGAPPER_ARCH='$AIRGAPPER_ARCH'"
    printf '%s' "$arch_value"
    return 0
  fi

  detected=$(normalize_arch "$(uname -m 2>/dev/null || true)")
  if [ -n "$detected" ]; then
    printf '%s' "$detected"
    return 0
  fi

  printf 'amd64'
}

resolve_install_dir() {
  if [ -n "${AIRGAPPER_INSTALL_DIR:-}" ]; then
    printf '%s' "$(expand_path "$AIRGAPPER_INSTALL_DIR")"
    return 0
  fi

  for candidate in "$HOME/.local/bin" "$HOME/bin" "/usr/local/bin"; do
    if [ -d "$candidate" ] && path_contains "$candidate"; then
      printf '%s' "$candidate"
      return 0
    fi
  done

  return 1
}

api_get() {
  curl -fsSL \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "$1"
}

resolve_release_json() {
  if [ -n "${AIRGAPPER_VERSION:-}" ]; then
    requested="$AIRGAPPER_VERSION"

    second_candidate=""
    case "$requested" in
      v*)
        second_candidate="${requested#v}"
        ;;
      *)
        second_candidate="v${requested}"
        ;;
    esac

    if json=$(api_get "$API_BASE_URL/releases/tags/$requested" 2>/dev/null); then
      printf '%s' "$json"
      return 0
    fi

    if [ -n "$second_candidate" ] && [ "$second_candidate" != "$requested" ]; then
      if json=$(api_get "$API_BASE_URL/releases/tags/$second_candidate" 2>/dev/null); then
        printf '%s' "$json"
        return 0
      fi
    fi

    fatal "version '$AIRGAPPER_VERSION' was not found in GitHub releases"
  fi

  api_get "$API_BASE_URL/releases/latest"
}

extract_tag_name() {
  printf '%s' "$1" | jq -r '.tag_name // empty'
}

extract_asset_url() {
  printf '%s' "$1" | jq -r \
    --arg os "$2" \
    --arg arch "$3" \
    --arg bin "$BINARY_NAME" \
    'first(
      .assets[]?.browser_download_url
      | select(test("/" + $bin + "_[^/]*_" + $os + "_" + $arch + "\\.tar\\.gz$"))
    ) // empty'
}

extract_checksums_url() {
  printf '%s' "$1" | jq -r \
    'first(
      .assets[]?.browser_download_url
      | select(test("checksums\\.txt$"))
    ) // empty'
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    digest_line=$(sha256sum "$1")
    printf '%s' "${digest_line%% *}"
  elif command -v shasum >/dev/null 2>&1; then
    digest_line=$(shasum -a 256 "$1")
    printf '%s' "${digest_line%% *}"
  elif command -v openssl >/dev/null 2>&1; then
    digest_line=$(openssl dgst -sha256 "$1")
    printf '%s' "${digest_line##* }"
  else
    return 1
  fi
}

# Looks up $2 (asset file name) in a GNU coreutils style checksums file ($1).
expected_checksum() {
  while IFS= read -r line; do
    entry_name="${line##* }"
    case "$entry_name" in
      "$2"|"*$2")
        printf '%s' "${line%% *}"
        return 0
        ;;
    esac
  done < "$1"
  return 1
}

verify_checksum() {
  archive="$1"
  checksums_url="$2"
  asset_name="$3"
  checksums_file="$4"

  curl -fsSL "$checksums_url" -o "$checksums_file" \
    || fatal "failed to download checksums from '$checksums_url'"

  expected=$(expected_checksum "$checksums_file" "$asset_name") \
    || fatal "no checksum entry for '$asset_name' in release checksums file"

  actual=$(sha256_file "$archive") \
    || fatal "no sha256 tool found (install sha256sum, shasum or openssl, or set AIRGAPPER_SKIP_CHECKSUM=1)"

  if [ "$expected" != "$actual" ]; then
    fatal "checksum mismatch for '$asset_name': expected '$expected', got '$actual'"
  fi

  log "Checksum verified: $actual"
}

extract_signature_url() {
  printf '%s' "$1" | jq -r \
    --arg name "$2.sigstore.json" \
    'first(
      .assets[]?
      | select(.name == $name)
      | .browser_download_url
    ) // empty'
}

# Fetches the cosign public key into $1 and prints the resulting path.
# AIRGAPPER_COSIGN_KEY may point at a local file or an alternative URL.
resolve_cosign_key() {
  key_source="${AIRGAPPER_COSIGN_KEY:-$COSIGN_KEY_URL}"

  case "$key_source" in
    http://*|https://*)
      curl -fsSL "$key_source" -o "$1" \
        || fatal "failed to download cosign public key from '$key_source'"
      printf '%s' "$1"
      ;;
    *)
      key_path=$(expand_path "$key_source")
      [ -f "$key_path" ] || fatal "cosign public key '$key_path' does not exist"
      printf '%s' "$key_path"
      ;;
  esac
}

verify_signature() {
  archive="$1"
  signature_url="$2"
  bundle_file="$3"
  key_file="$4"

  curl -fsSL "$signature_url" -o "$bundle_file" \
    || fatal "failed to download signature bundle from '$signature_url'"

  key_file=$(resolve_cosign_key "$key_file")

  # Verification contacts the Rekor transparency log by default, which hosts
  # without internet access cannot reach.
  if is_true "${AIRGAPPER_COSIGN_OFFLINE:-0}"; then
    cosign_output=$(cosign verify-blob --key "$key_file" --bundle "$bundle_file" \
      --insecure-ignore-tlog=true "$archive" 2>&1) \
      || fatal "cosign verification failed: $cosign_output"
  else
    cosign_output=$(cosign verify-blob --key "$key_file" --bundle "$bundle_file" \
      "$archive" 2>&1) \
      || fatal "cosign verification failed: $cosign_output"
  fi

  log "Signature verified with cosign."
}

confirm_install() {
  if is_true "$ASSUME_YES_VALUE"; then
    log "Skipping approval prompt because AIRGAPPER_YES is enabled."
    return 0
  fi

  if [ ! -r /dev/tty ]; then
    fatal "no interactive terminal for approval. Set AIRGAPPER_YES=1 to pre-approve"
  fi

  printf '%sProceed with installation? [y/N]: %s' "$COLOR_PROMPT" "$COLOR_RESET"
  if ! IFS= read -r answer < /dev/tty; then
    fatal "could not read confirmation input"
  fi

  normalized=$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')
  case "$normalized" in
    y|yes)
      return 0
      ;;
    *)
      fatal "installation aborted by user"
      ;;
  esac
}

main() {
  init_colors

  require_cmd curl
  require_cmd tar
  require_cmd uname
  require_cmd mktemp
  require_cmd grep
  require_cmd jq

  resolved_os=$(resolve_os)
  resolved_arch=$(resolve_arch)

  if ! install_dir=$(resolve_install_dir); then
    fatal "no suitable install directory found. Create one of '$HOME/.local/bin', '$HOME/bin', '/usr/local/bin' and add it to PATH, or set AIRGAPPER_INSTALL_DIR"
  fi

  log "Resolving release metadata from GitHub API..."
  release_json=$(resolve_release_json)
  release_tag=$(extract_tag_name "$release_json")
  [ -n "$release_tag" ] || fatal "failed to parse release tag from GitHub API response: $release_json"

  release_version="$release_tag"
  case "$release_version" in
    v*)
      release_version="${release_version#v}"
      ;;
  esac

  asset_url=$(extract_asset_url "$release_json" "$resolved_os" "$resolved_arch")
  [ -n "$asset_url" ] || fatal "no release archive found for os='$resolved_os' arch='$resolved_arch' in release '$release_tag'"

  asset_name="${asset_url##*/}"

  checksums_url=""
  if is_true "${AIRGAPPER_SKIP_CHECKSUM:-0}"; then
    log "Checksum verification disabled via AIRGAPPER_SKIP_CHECKSUM."
  else
    checksums_url=$(extract_checksums_url "$release_json")
    [ -n "$checksums_url" ] || fatal "no checksums file in release '$release_tag'. Set AIRGAPPER_SKIP_CHECKSUM=1 to install without verification"
  fi

  # Signature verification is best effort: it needs cosign on the host, and
  # releases published before signing was introduced carry no bundle.
  signature_url=""
  if is_true "${AIRGAPPER_SKIP_SIGNATURE:-0}"; then
    log "Signature verification disabled via AIRGAPPER_SKIP_SIGNATURE."
  elif ! command -v cosign >/dev/null 2>&1; then
    log "cosign not found, skipping signature verification."
  else
    signature_url=$(extract_signature_url "$release_json" "$asset_name")
    if [ -z "$signature_url" ]; then
      log "Release '$release_tag' ships no signature for '$asset_name', skipping signature verification."
    fi
  fi

  log "Calculated installation settings:"
  log "  Repository:   ${REPO_OWNER}/${REPO_NAME}"
  log "  Version tag:  $release_tag"
  log "  Version:      $release_version"
  log "  OS:           $resolved_os"
  log "  Arch:         $resolved_arch"
  log "  Install dir:  $install_dir"
  log "  Archive URL:  $asset_url"
  if [ -n "$checksums_url" ]; then
    log "  Checksums:    $checksums_url"
  fi
  if [ -n "$signature_url" ]; then
    log "  Signature:    $signature_url"
    log "  Cosign key:   ${AIRGAPPER_COSIGN_KEY:-$COSIGN_KEY_URL}"
  fi

  confirm_install

  if [ ! -d "$install_dir" ]; then
    log "Install directory does not exist. Creating: $install_dir"
    mkdir -p "$install_dir" || fatal "failed to create install directory '$install_dir'"
  fi

  [ -w "$install_dir" ] || fatal "install directory '$install_dir' is not writable"

  tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/airgapper-install.XXXXXX")
  archive_path="$tmp_dir/$asset_name"

  cleanup() {
    rm -rf "$tmp_dir"
  }
  trap cleanup EXIT INT TERM

  log "Downloading release archive..."
  if is_true "$QUIET_VALUE"; then
    curl -fsSL "$asset_url" -o "$archive_path"
  else
    curl -fL "$asset_url" -o "$archive_path"
  fi

  if [ -n "$checksums_url" ]; then
    log "Verifying checksum..."
    verify_checksum "$archive_path" "$checksums_url" "$asset_name" "$tmp_dir/checksums.txt"
  fi

  if [ -n "$signature_url" ]; then
    log "Verifying signature with cosign..."
    verify_signature "$archive_path" "$signature_url" \
      "$tmp_dir/signature.sigstore.json" "$tmp_dir/cosign.pub"
  fi

  binary_file="$BINARY_NAME"
  if [ "$resolved_os" = "windows" ]; then
    binary_file="${BINARY_NAME}.exe"
  fi

  log "Locating '$binary_file' inside archive..."
  binary_in_archive=$(tar -tzf "$archive_path" | grep -E "/${binary_file}\$|^${binary_file}\$" | head -n 1 || true)
  [ -n "$binary_in_archive" ] || fatal "could not find '$binary_file' in downloaded archive"

  log "Extracting binary from archive path: $binary_in_archive"
  tar -xzf "$archive_path" -C "$tmp_dir" "$binary_in_archive"

  source_binary="$tmp_dir/$binary_in_archive"
  [ -f "$source_binary" ] || fatal "expected extracted binary at '$source_binary'"

  target_binary="$install_dir/$binary_file"
  log "Installing binary to: $target_binary"
  chmod 0755 "$source_binary"
  # Staged inside the install dir first so a cross-device copy never leaves a
  # partially written binary on PATH.
  mv "$source_binary" "$target_binary.new"
  mv "$target_binary.new" "$target_binary"

  log "Installation complete."
  log "Run '$BINARY_NAME --help' to verify the installation."
  log "Installed binary path: $target_binary"
  log "Uninstall using 'rm $target_binary'"
}

main "$@"
