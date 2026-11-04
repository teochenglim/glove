#!/usr/bin/env bash
# Verify one release's ACs: every AC row has >=2 tests and they pass.
#   scripts/release-check.sh 0.1.0
set -uo pipefail
cd "$(dirname "$0")/.."
REL="${1:?usage: release-check.sh X.Y.Z}"
FILE="RELEASE/v$REL.md"
[ -f "$FILE" ] || { echo "no $FILE"; exit 2; }
echo "== v$REL: $(grep -cE '^\| AC[0-9]' "$FILE") ACs"
fail=0
for id in $(grep -oE '^\| AC[0-9]+\.[0-9]+\.[0-9]+' "$FILE" | tr -d '| '); do
  slug=$(echo "${id#AC}" | tr . _)
  n=$(find . -name '*.go' -not -path './bin/*' -not -path './dist/*' -print0 2>/dev/null \
      | xargs -0 grep -hoE "func Test(AC|Pg|Live)_${slug}_[A-Za-z0-9_]+|t\.Run\(\"ac_${slug}_[a-z0-9_]+\"" 2>/dev/null \
      | sort -u | wc -l)
  printf '  %-12s %2d tests\n' "$id" "$n"; [ "$n" -ge 2 ] || fail=1
done
MA=${REL%%.*}; REST=${REL#*.}; MI=${REST%%.*}
# Runs the tests whose names carry the release prefix (TestAC_0_1_*; a t.Run suite's
# parent function must carry the same prefix to be selected).
go test -count=1 ./... -run "TestAC_${MA}_${MI}_" 2>&1 | tail -5 || fail=1
exit $fail
