#!/usr/bin/env bash
# Traceability gate (PRD §7 roadmap -> RELEASE/):
#   1. every acceptance-test bullet of a PRD §7 version block that HAS a release
#      file is cited by >=1 AC (bullet ids are derived as PRD-<version>-<n>,
#      n = order inside the block). Versions without a RELEASE/vX.Y.Z.md yet are
#      not enforced: writing that file (ACs before code) is what turns their
#      bullets into requirements.
#   2. every AC cites >=1 known PRD anchor: a PRD-<ver>-<n> bullet id, or a §<n>
#      reference whose integer part is a real PRD.md section heading
#   3. every AC is governed by >=2 distinct tests
# AC rows must cite explicitly. Go tests: func TestAC_<maj>_<min>_<n>_<slug>
# (AC<maj.min.n>) or t.Run("ac_<maj>_<min>_<n>_<slug>", ...) inside a parent
# whose name also starts TestAC_<maj>_<min>_<n>.
# Releases whose Status is "planned" are checked for requirement coverage only;
# their tests are written when the release is built.
set -eu
cd "$(dirname "$0")/.."
MIN="${AC_MIN_TESTS:-2}"
fail=0

# PRD acceptance bullets: checkbox lines inside a §7 roadmap version block
# ("### <M.m.0> — Theme", opened by **Acceptance tests**, closed by **Out of
# scope** or the next heading). §10.5-style blocks ("10.5") do not match the
# version pattern and are correctly ignored.
all_reqs=$(awk '
  /^## /                      { inacc = 0 }
  /^### [0-9]+\.[0-9]+\.0 /   { ver = $2; n = 0; inacc = 0; next }
  /^\*\*Acceptance tests\*\*/ { inacc = 1; next }
  /^\*\*Out of scope\*\*/     { inacc = 0 }
  inacc && /^- \[ \]/         { n++; print "PRD-" ver "-" n }
' PRD.md | sort -u)
[ -n "$all_reqs" ] || { echo "no acceptance bullets found in PRD.md version blocks"; exit 1; }

# Only versions with a release file are coverage-enforced.
vers=$(for f in RELEASE/v*.md; do [ -e "$f" ] || continue; basename "$f" .md | sed 's/^v//'; done | sort -u)
reqs=$(echo "$all_reqs" | grep -E "^PRD-($(echo "$vers" | tr '.' '\.' | paste -sd'|' -))-")
[ -n "$reqs" ] || reqs=""

# Real PRD sections ("## 1. Executive Summary" ...), for §-ref validation.
sections=$(grep -oE '^## [0-9]+' PRD.md | grep -oE '[0-9]+' | sort -un)
rows=$(grep -hE '^\| AC[0-9]+\.[0-9]+\.[0-9]+ ' RELEASE/v*.md || true)
active_rows=$(for f in RELEASE/v*.md; do grep -qE '^\*\*Status:\*\* planned' "$f" && continue; grep -hE '^\| AC[0-9]+\.[0-9]+\.[0-9]+ ' "$f"; done || true)
[ -n "$rows" ] || { echo "no AC rows in RELEASE/"; exit 1; }

# 1: every PRD bullet covered; unknown bullet ids also fail.
# comm-based set checks: per-item `echo | grep -qx` loops die of SIGPIPE
# (grep exits on the first match; the writer gets EPIPE).
cited_prd=$(echo "$rows" | grep -oE 'PRD-[0-9]+\.[0-9]+\.0-[0-9]+' | sort -u || true)
uncovered=$(comm -23 <(echo "$reqs") <(echo "$cited_prd"))
if [ -n "$uncovered" ]; then
  echo "$uncovered" | sed 's/$/ (no AC cites it)/' | sed 's/^/UNCOVERED PRD bullet /'
  fail=1
fi
unknown=$(comm -13 <(echo "$all_reqs") <(echo "$cited_prd"))
if [ -n "$unknown" ]; then
  echo "$unknown" | sed 's/$/ cited by an AC/' | sed 's/^/UNKNOWN PRD bullet /'
  fail=1
fi

# 2: every AC row cites >=1 known anchor.
while IFS= read -r row; do
  id=$(echo "$row" | grep -oE '^\| AC[0-9]+\.[0-9]+\.[0-9]+' | tr -d '| ')
  row_bullets=$(echo "$row" | grep -oE 'PRD-[0-9]+\.[0-9]+\.0-[0-9]+' | sort -u)
  secs=$(echo "$row" | grep -oE '§[0-9]+' | tr -d '§' | sort -un)
  if [ -z "$row_bullets" ] && [ -z "$secs" ]; then
    echo "AC $id cites no PRD anchor (PRD-<ver>-<n> bullet or §<n>)"
    fail=1
    continue
  fi
  bad_sec=$(comm -13 <(echo "$sections") <(echo "$secs") || true)
  if [ -n "$bad_sec" ]; then
    echo "$bad_sec" | sed "s/^/AC $id cites unknown PRD section §/"
    fail=1
  fi
done <<< "$rows"

# 3: tests per AC (Go sources only; bin/ and dist/ excluded)
count_tests() { # $1 = AC id e.g. AC0.1.3
  local id="$1" slug
  slug=$(echo "${id#AC}" | tr . _)
  find . -name '*.go' -not -path './bin/*' -not -path './dist/*' -print0 2>/dev/null \
    | xargs -0 grep -hoE "func Test(AC|Pg|Live)_${slug}_[A-Za-z0-9_]+|t\.Run\(\"ac_${slug}_[a-z0-9_]+\"" 2>/dev/null \
    | sort -u | wc -l | tr -d ' '
}
total=0; bad=0
for id in $(echo "$active_rows" | grep -oE '^\| AC[0-9]+\.[0-9]+\.[0-9]+' | tr -d '| ' | sort -u); do
  n=$(count_tests "$id"); total=$((total+1))
  if [ "$n" -lt "$MIN" ]; then echo "AC $id has $n test(s), needs >= $MIN"; bad=$((bad+1)); fi
done
[ "$bad" -eq 0 ] || fail=1
echo "ac-map: $total active ACs, $(echo "$reqs" | wc -l | tr -d ' ') enforced PRD bullets (of $(echo "$all_reqs" | wc -l | tr -d ' ') in the PRD), $bad under-tested"
exit $fail
