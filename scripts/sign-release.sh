#!/usr/bin/env bash
# sign-release.sh — signing steps for published glove artifacts (PRD §7
# 0.6.0, §9: signed binaries with published SHA-256).
#
# Signing needs maintainer certificates that must never live in the repo or
# CI secrets until the maintainer chooses to add them. This script runs
# LOCALLY on the tagged artifacts (download from the GitHub release or
# dist/), verifies the checksums first, and then:
#
#   1. macOS: codesign + notarytool notarization (Apple Developer ID).
#   2. Windows: signtool Authenticode signing.
#
# Usage:
#   scripts/sign-release.sh <dist-dir>
#
# Required env (only for the platform you sign):
#   macOS:  APPLE_ID, APPLE_TEAM_ID, APPLE_APP_SPECIFIC_PASSWORD,
#           MACOS_SIGNING_IDENTITY   (e.g. "Developer ID Application: …")
#   Windows: WIN_CERT_FILE (PKCS#12 .pfx), WIN_CERT_PASS, (signtool.exe on PATH)
#
# Every step is skipped with an explicit message when its env is absent —
# unsigned artifacts still pass the checksum gate; the release file records
# which platforms were signed.
set -euo pipefail
cd "$(dirname "$0")/.."

DIR="${1:?usage: sign-release.sh <dist-dir>}"
[ -d "$DIR" ] || { echo "no such dir: $DIR" >&2; exit 2; }

echo "== verifying checksums in $DIR =="
if [ -f "$DIR/checksums.txt" ]; then
  (cd "$DIR" && sha256sum -c checksums.txt)
else
  for f in "$DIR"/*.sha256; do
    [ -e "$f" ] || { echo "no .sha256 files in $DIR" >&2; exit 2; }
    (cd "$DIR" && sha256sum -c "$(basename "$f")")
  done
fi

signed="none"

sign_macos() {
  command -v codesign >/dev/null || { echo "codesign not found — skipping macOS signing"; return 0; }
  [ -n "${MACOS_SIGNING_IDENTITY:-}" ] || { echo "MACOS_SIGNING_IDENTITY not set — skipping macOS signing"; return 0; }
  : "${APPLE_ID:?APPLE_ID required for notarization}"
  : "${APPLE_TEAM_ID:?APPLE_TEAM_ID required for notarization}"
  : "${APPLE_APP_SPECIFIC_PASSWORD:?APPLE_APP_SPECIFIC_PASSWORD required for notarization}"
  for d in "$DIR"/glove-*darwin-*; do
    [ -e "$d" ] || continue
    codesign --force --options runtime --timestamp \
      --sign "$MACOS_SIGNING_IDENTITY" "$d"
    # Notarize the tarball (staple keeps the approval with the artifact).
    xcrun notarytool submit "$d.tar.gz" \
      --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" \
      --password "$APPLE_APP_SPECIFIC_PASSWORD" --wait
    xcrun stapler staple "$d" 2>/dev/null || true
    echo "signed + notarized: $d"
    signed="darwin"
  done
}

sign_windows() {
  command -v signtool >/dev/null 2>&1 || { echo "signtool not found — skipping Windows signing"; return 0; }
  [ -n "${WIN_CERT_FILE:-}" ] || { echo "WIN_CERT_FILE not set — skipping Windows signing"; return 0; }
  for e in "$DIR"/glove-*windows-*.exe; do
    [ -e "$e" ] || continue
    signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 \
      /f "$WIN_CERT_FILE" /p "${WIN_CERT_PASS:?WIN_CERT_PASS required}" "$e"
    echo "Authenticode-signed: $e"
    signed="windows"
  done
}

sign_macos
sign_windows

# After signing, refresh the checksums so the published hashes cover the
# signed bytes (signing mutates the binaries).
echo "== refreshing checksums after signing ($signed) =="
( cd "$DIR" && rm -f checksums.txt *.sha256
  for f in glove-*.tar.gz; do shasum -a 256 "$f" > "$f.sha256"; cat "$f.sha256"; done
  cat *.sha256 > checksums.txt )
echo "done — upload the refreshed artifacts + checksums to the release"
