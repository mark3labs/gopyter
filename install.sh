#!/usr/bin/env bash
# gopyter install script.
#
# Downloads a released `gopyter` binary, verifies its SHA-256 checksum
# against the release's checksum file, and installs it.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mark3labs/gopyter/master/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/mark3labs/gopyter/master/install.sh | bash -s -- --version v0.1.0
#   bash install.sh [--bin-dir <dir>] [--version <tag>] [--no-checks]
#
# Environment:
#   GOPYTER_INSTALL_DIR   Same as --bin-dir.
#   GOPYTER_VERSION       Same as --version.
#   GOPYTER_RELEASES_URL  Base URL of the releases (default: the GitHub
#                         releases of mark3labs/gopyter). For mirrors, and for
#                         scripts/install_test.go. The layout under it must be
#                         <base>/download/<tag>/<asset>.
#
# The script refuses to install a binary it cannot verify. It never falls
# back to an unverified download.
set -euo pipefail

# ── Output helpers ───────────────────────────────────────────────────────────
if [ -t 1 ] && command -v tput >/dev/null 2>&1 && tput colors >/dev/null 2>&1; then
  BOLD=$(tput bold); RESET=$(tput sgr0)
  RED=$(tput setaf 1); GREEN=$(tput setaf 2)
  YELLOW=$(tput setaf 3); CYAN=$(tput setaf 6)
else
  BOLD=""; RESET=""; RED=""; GREEN=""; YELLOW=""; CYAN=""
fi

info()    { printf '%s  %s\n' "${CYAN}→${RESET}" "$*"; }
success() { printf '%s  %s\n' "${GREEN}✓${RESET}" "$*"; }
warn()    { printf '%s  %s\n' "${YELLOW}!${RESET}" "$*"; }
die()     { printf '%s  %s\n' "${RED}✗${RESET}" "$*" >&2; exit 1; }
header()  { printf '\n%s%s%s\n\n' "${BOLD}" "$*" "${RESET}"; }

# ── Defaults ─────────────────────────────────────────────────────────────────
REPO="mark3labs/gopyter"
BIN_NAME="gopyter"
RELEASES_URL="${GOPYTER_RELEASES_URL:-https://github.com/${REPO}/releases}"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
VERSION="${GOPYTER_VERSION:-}"   # empty = latest release
BIN_DIR="${GOPYTER_INSTALL_DIR:-}" # empty = auto-detect
RUN_CHECKS=true

usage() {
  cat <<EOF
Usage: install.sh [options]

Options:
  --bin-dir <dir>   Directory to install gopyter into (default: ~/.local/bin)
  --version <tag>   Release tag to install, e.g. v0.1.0 (default: latest)
  --no-checks       Skip the host checks (Go toolchain, gopls)
  -h, --help        Show this help

Release binaries are published for Linux and macOS on amd64 and arm64.
On other platforms, install with: go install github.com/${REPO}@latest
EOF
}

# ── Arguments ────────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "$1" in
    --bin-dir)
      [ $# -ge 2 ] || die "--bin-dir needs a value"
      BIN_DIR="$2"; shift 2 ;;
    --version)
      [ $# -ge 2 ] || die "--version needs a value"
      VERSION="$2"; shift 2 ;;
    --no-checks) RUN_CHECKS=false; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown option: $1 (see --help)" ;;
  esac
done

# ── Platform ─────────────────────────────────────────────────────────────────
# Must match the goos/goarch lists in .goreleaser.yaml.
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    *) die "Unsupported OS: $(uname -s). Release binaries cover Linux and macOS; use: go install github.com/${REPO}@latest" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "Unsupported architecture: $(uname -m). Release binaries cover amd64 and arm64; use: go install github.com/${REPO}@latest" ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

# ── Install directory ────────────────────────────────────────────────────────
resolve_bin_dir() {
  if [ -n "$BIN_DIR" ]; then
    printf '%s\n' "$BIN_DIR"
    return
  fi
  local dir
  for dir in "$HOME/.local/bin" "$HOME/bin" "/usr/local/bin"; do
    if [ -d "$dir" ] && [ -w "$dir" ]; then
      printf '%s\n' "$dir"
      return
    fi
  done
  printf '%s\n' "$HOME/.local/bin"
}

# ── HTTP ─────────────────────────────────────────────────────────────────────
fetch() { # fetch <url> <dest>
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    die "Neither curl nor wget is available. Install one and retry."
  fi
}

latest_version() {
  local tmp tag
  tmp=$(mktemp)
  fetch "$API_URL" "$tmp" 2>/dev/null || { rm -f "$tmp"; die "Could not query the latest release. Use --version to pin one."; }
  tag=$(grep '"tag_name"' "$tmp" | head -n 1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  rm -f "$tmp"
  [ -n "$tag" ] || die "Could not determine the latest release. Use --version to pin one."
  printf '%s\n' "$tag"
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "SHA-256 verification needs sha256sum or shasum."
  fi
}

# ── Install ──────────────────────────────────────────────────────────────────
# Asset names follow goreleaser's defaults in .goreleaser.yaml. The tag has a
# leading "v"; the file names do not:
#   gopyter_<version>_<os>_<arch>.tar.gz
#   gopyter_<version>_checksums.txt
install_from_release() {
  local tag="$1" platform="$2" bin_dir="$3"
  local version="${tag#v}"
  local asset="${BIN_NAME}_${version}_${platform}.tar.gz"
  local sums="${BIN_NAME}_${version}_checksums.txt"
  local base="${RELEASES_URL}/download/${tag}"

  TMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TMP_DIR"' EXIT

  info "Downloading ${asset}"
  fetch "${base}/${asset}" "${TMP_DIR}/${asset}" 2>/dev/null \
    || die "Could not download ${base}/${asset}"

  fetch "${base}/${sums}" "${TMP_DIR}/${sums}" 2>/dev/null \
    || die "Could not download ${sums}. Refusing to install an unverified binary."

  local expected actual
  expected=$(awk -v a="$asset" '$2 == a { print $1; exit }' "${TMP_DIR}/${sums}")
  [ -n "$expected" ] || die "${sums} has no entry for ${asset}. Refusing to install an unverified binary."
  actual=$(sha256 "${TMP_DIR}/${asset}")
  [ "$actual" = "$expected" ] || die "Checksum mismatch for ${asset}. Refusing to install."
  success "Checksum verified"

  command -v tar >/dev/null 2>&1 || die "tar is required to extract ${asset}."
  mkdir -p "${TMP_DIR}/x"
  tar -xzf "${TMP_DIR}/${asset}" -C "${TMP_DIR}/x" "$BIN_NAME" 2>/dev/null \
    || die "${asset} does not contain ${BIN_NAME}."

  mkdir -p "$bin_dir" || die "Cannot create ${bin_dir}. Use --bin-dir to choose another directory."
  [ -w "$bin_dir" ] || die "${bin_dir} is not writable. Use --bin-dir, or run with sudo."
  install -m 0755 "${TMP_DIR}/x/${BIN_NAME}" "${bin_dir}/${BIN_NAME}"
  success "Installed ${BIN_NAME} ${tag} → ${bin_dir}/${BIN_NAME}"
}

# ── Host checks ──────────────────────────────────────────────────────────────
# Warnings only. The binary is installed either way; these tell the user
# what gopyter needs at runtime.
check_path() {
  case ":${PATH}:" in
    *":$1:"*) ;;
    *)
      echo ""
      warn "$1 is not on your \$PATH. Add this line to your shell rc file:"
      printf '\n    %sexport PATH="%s:$PATH"%s\n' "${BOLD}" "$1" "${RESET}"
      ;;
  esac
}

# gopyter compiles every cell with the Go toolchain, so Go is required.
check_go() {
  if command -v go >/dev/null 2>&1; then
    success "Go toolchain found ($(go env GOVERSION 2>/dev/null || echo go))"
  else
    echo ""
    warn "Go is not installed. gopyter compiles cells with the Go toolchain, so it cannot run them without Go."
    echo "    Install Go from https://go.dev/dl/ and make sure 'go' is on your \$PATH."
  fi
}

# gopls powers code completion; without it gopyter falls back to basic
# completion.
check_gopls() {
  if command -v gopls >/dev/null 2>&1; then
    success "gopls found (IDE-style completion enabled)"
  else
    echo ""
    warn "gopls is not installed; completion falls back to keywords and notebook identifiers."
    printf '    For IDE-style completion: %sgo install golang.org/x/tools/gopls@latest%s\n' "${BOLD}" "${RESET}"
  fi
}

# ── Main ─────────────────────────────────────────────────────────────────────
main() {
  header "Installing gopyter"

  local platform bin_dir
  platform=$(detect_platform)
  info "Platform: ${platform}"

  bin_dir=$(resolve_bin_dir)

  if [ -z "$VERSION" ]; then
    info "Resolving the latest release"
    VERSION=$(latest_version)
  fi
  case "$VERSION" in v*) ;; *) VERSION="v${VERSION}" ;; esac
  info "Version: ${VERSION}"

  install_from_release "$VERSION" "$platform" "$bin_dir"

  check_path "$bin_dir"
  if [ "$RUN_CHECKS" = true ]; then
    check_go
    check_gopls
  fi

  echo ""
  success "Done. Run '${BIN_NAME}' to open a notebook, or '${BIN_NAME} --help'."
  echo ""
}

main "$@"
