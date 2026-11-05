#!/bin/sh
# Fill the Homebrew formula's sha256 lines from a release's checksums.txt
# (packaging/homebrew/README.md step 2). Usage: update-homebrew-sha.sh <version>
#   <version> like 0.9.0 — checksums are fetched from the published release.
set -e
VER="$1"
[ -n "$VER" ] || { echo "usage: $0 <version>" >&2; exit 1; }
case "$VER" in v*) ;; *) VER="v$VER" ;; esac

FORMULA="$(dirname "$0")/../packaging/homebrew/glove.rb"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "https://github.com/teochenglim/glove/releases/download/${VER}/checksums.txt" -o "$TMP/checksums.txt"
else
  wget -qO "$TMP/checksums.txt" "https://github.com/teochenglim/glove/releases/download/${VER}/checksums.txt"
fi

# The formula interleaves url → sha256 per platform; walk the lines, track
# the current platform from the url, replace the next sha256 with the
# matching checksums.txt entry.
python3 - "$TMP/checksums.txt" "$FORMULA" "$VER" <<'EOF'
import re, sys
sums = {}
for line in open(sys.argv[1]):
    parts = line.split()
    if len(parts) == 2:
        sums[parts[1]] = parts[0]
ver = sys.argv[3]
lines = open(sys.argv[2]).read().split("\n")
current = None
replaced = 0
for i, line in enumerate(lines):
    m = re.search(r"glove-v\{version\}-(darwin|linux)-(arm64|amd64)\.tar\.gz", line)
    if m:
        current = f"{m.group(1)}-{m.group(2)}"
        continue
    m2 = re.match(r'(\s*)sha256 "([0-9a-f]{64})"', line)
    if m2 and current:
        key = f"glove-{ver}-{current}.tar.gz"
        if key not in sums:
            sys.exit(f"checksums.txt has no entry for {key}")
        lines[i] = f'{m2.group(1)}sha256 "{sums[key]}"'
        replaced += 1
        current = None
if replaced != 4:
    sys.exit(f"expected to replace 4 sha256 lines, replaced {replaced}")
open(sys.argv[2], "w").write("\n".join(lines))
EOF
echo "formula sha256 lines filled from ${VER}:"
grep -n 'sha256 "' "$FORMULA"
