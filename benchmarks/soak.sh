#!/usr/bin/env bash
# soak.sh — mechanical soak between the accelerated CI gate and a real 24h
# live session (PRD §7 0.6.0). Spawns N sequential hook processes with
# alternating allow/block payloads through a throwaway HOME, and fails on
# any exit-code deviation or malformed decision envelope.
#
# Usage: benchmarks/soak.sh [iterations]     # default 500
set -euo pipefail
cd "$(dirname "$0")/.."

N="${1:-500}"
BIN="${BIN:-./bin/glove}"
[ -x "$BIN" ] || { echo "build first: make build" >&2; exit 2; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export HOME="$WORK"
export GLOVE_CONFIG_DIR="$WORK/cfg" GLOVE_DATA_DIR="$WORK/data"
mkdir -p "$WORK/cfg"
printf '[general]\nmode = "offline"\n' > "$WORK/cfg/policy.toml"

allow='{"tool_name":"Bash","tool_input":{"command":"ls -la"}}'
block='{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}'

echo "soak: $N iterations through $BIN (offline policy)"
fails=0
for i in $(seq 1 "$N"); do
  if [ $((i % 2)) -eq 0 ]; then payload="$allow"; want=0; else payload="$block"; want=2; fi
  out="$(printf '%s' "$payload" | "$BIN" hook claude-code 2>/dev/null)" && code=0 || code=$?
  if [ "$code" -ne "$want" ]; then
    echo "FAIL iter $i: exit $code, want $want (stdout: $out)"
    fails=$((fails + 1))
  fi
  case "$out" in
    *'"permissionDecision"'*) ;;       # envelope present
    *) echo "FAIL iter $i: no decision envelope: $out"; fails=$((fails + 1)) ;;
  esac
done

records="$(wc -l < "$GLOVE_DATA_DIR/audit.jsonl" | tr -d ' ')"
if [ "$records" -ne "$N" ]; then
  echo "FAIL: audit holds $records records, want $N"
  fails=$((fails + 1))
fi

if [ "$fails" -ne 0 ]; then
  echo "soak FAILED: $fails deviation(s) over $N iterations"
  exit 1
fi
echo "soak PASSED: $N iterations, $records audit records, zero deviations"
