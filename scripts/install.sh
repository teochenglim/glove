#!/bin/sh
# glove installer (v0.9.0, PRD §9). Downloads a pinned release artifact,
# verifies its SHA-256 against the release's checksums.txt BEFORE extracting
# or installing anything, then installs the glove binary.
#
# Honesty note (RELEASE/v0.9.0.md): this script verifies the ARTIFACT, not
# itself — run it only from a source you trust. It never bypasses glove's
# fail-closed story; it only unpacks a release you checksum-verified.
#
# Usage:
#   scripts/install.sh [version]        # version like 0.9.0 or v0.9.0
# Env:
#   GLOVE_INSTALL_DIR      destination (default /usr/local/bin)
#   GLOVE_INSTALL_BASE_URL override the release base (tests / airgap mirrors)
#   GLOVE_INSTALL_VERSION  version without passing an argument
set -e

REPO=teochenglim/glove
BASE_URL="${GLOVE_INSTALL_BASE_URL:-https://github.com/${REPO}/releases/download}"
INSTALL_DIR="${GLOVE_INSTALL_DIR:-/usr/local/bin}"

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) echo "install.sh: unsupported OS '$(uname -s)' (windows is deferred — build from source)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64 ;;
  *) echo "install.sh: unsupported architecture '$(uname -m)'" >&2; exit 1 ;;
esac

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    echo "install.sh: need curl or wget" >&2
    exit 1
  fi
}

VERSION="${1:-${GLOVE_INSTALL_VERSION:-}}"
if [ -z "$VERSION" ] && command -v curl >/dev/null 2>&1; then
  # Resolve the latest release tag from the GitHub redirect (HEAD, no API).
  VERSION=$(curl -fsSI "https://github.com/${REPO}/releases/latest" 2>/dev/null \
    | awk 'tolower($1) == "location:" { sub(/.*tag\//, "", $2); gsub(/\r/, "", $2); print $2 }' \
    | head -1)
fi
if [ -z "$VERSION" ]; then
  echo "install.sh: pass a version (e.g. scripts/install.sh 0.9.0)" >&2
  exit 1
fi
case "$VERSION" in
  v*) ;;
  *) VERSION="v${VERSION}" ;;
esac

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fetch "${BASE_URL}/${VERSION}/checksums.txt" "${TMP}/checksums.txt"
ASSET="glove-${VERSION}-${OS}-${ARCH}"
fetch "${BASE_URL}/${VERSION}/${ASSET}.tar.gz" "${TMP}/${ASSET}.tar.gz"

# Verify BEFORE extraction or install (the verify-then-unpack order is the
# point: no release byte is executed or unpacked until its hash matches).
if command -v sha256sum >/dev/null 2>&1; then
  GOT=$(sha256sum "${TMP}/${ASSET}.tar.gz" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  GOT=$(shasum -a 256 "${TMP}/${ASSET}.tar.gz" | awk '{print $1}')
else
  echo "install.sh: need sha256sum or shasum to verify the download" >&2
  exit 1
fi
WANT=$(awk -v f="${ASSET}.tar.gz" '$2 == f {print $1}' "${TMP}/checksums.txt")
if [ -z "$WANT" ]; then
  echo "install.sh: ${ASSET}.tar.gz not listed in checksums.txt — refusing" >&2
  exit 1
fi
if [ "$GOT" != "$WANT" ]; then
  echo "install.sh: CHECKSUM MISMATCH for ${ASSET}.tar.gz — refusing to install" >&2
  echo "  want $WANT" >&2
  echo "  got  $GOT" >&2
  exit 1
fi
echo "checksum ok: ${ASSET}.tar.gz"

tar -xzf "${TMP}/${ASSET}.tar.gz" -C "$TMP"
BIN="${TMP}/${ASSET}/${ASSET}"
if [ ! -f "$BIN" ]; then
  echo "install.sh: unexpected archive layout (no ${ASSET}/${ASSET}) — refusing" >&2
  exit 1
fi

if ! mkdir -p "$INSTALL_DIR" 2>/dev/null && [ ! -d "$INSTALL_DIR" ]; then
  echo "install.sh: cannot create $INSTALL_DIR — retry with GLOVE_INSTALL_DIR=\$HOME/.local/bin" >&2
  exit 1
fi
if ! cp "$BIN" "${INSTALL_DIR}/glove" 2>/dev/null; then
  echo "install.sh: cannot write ${INSTALL_DIR}/glove — retry with GLOVE_INSTALL_DIR=\$HOME/.local/bin" >&2
  exit 1
fi
chmod +x "${INSTALL_DIR}/glove"
echo "installed ${VERSION} -> ${INSTALL_DIR}/glove"
"${INSTALL_DIR}/glove" version
