# Troubleshooting

The per-agent guides carry agent-specific pitfalls; this page is the
cross-cutting pass. First move for every problem:

```bash
glove doctor          # per-agent chain health, exit 1 if anything is UNHEALTHY
glove list            # the whole matrix with hook status
```

## Nothing blocks in my agent

1. `glove doctor` — the agent's row must say **hook entry installed ✓**.
   If it says NOT installed: `glove install <agent>` and restart the agent
   (hooks are read at session start).
2. If it says **UNHEALTHY** — the config was corrupted or hand-edited;
   follow the row's advice, usually `glove install <agent>` to regenerate.
3. `glove check "rm -rf /"` — must print `block`. If it doesn't, the
   engine itself has a problem; run `make check` if you built from source.
4. `echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | glove hook <agent> ; echo $?`
   — must print a deny envelope and exit 2.
5. Agent-specific trust surfaces: Codex `/hooks` review, Gemini
   `hooksConfig.enabled` layers, Kilo `KILO_PURE=1`, Cursor project-level
   `hooks.json` override — each covered at the bottom of its
   [install guide](install/).

## `command not found: glove` inside hooks

The hook/shim resolves `glove` from `PATH`. Keep the binary on a standard
PATH (`/usr/local/bin`), not a project-local directory. Windows: the shim
spawns `glove.exe`.

## A review blocked my call (cursor / gemini)

Those agents cannot enforce "ask" on their hook surfaces, so glove renders
review as **deny** — it only ever tightens. See the reasoning with
`glove check "<the command>"` and decide manually. To change the underlying
thresholds, use the policy engine ([policy cookbook](policy-cookbook.md)).

## Latency

```bash
glove doctor --bench hook     # budget: 15ms median (PRD §8)
```

Full benchmark suite and repro instructions: [benchmarks/](../benchmarks/README.md).

## Audit log questions

- Zero plaintext by default; records carry a SHA-256 `input_hash` only.
  `--audit-verbose` adds redacted input. See the
  [audit section of the Claude Code guide](install/claude-code.md) and
  `glove audit query --decision block --since 24h`.
- Rotation: 10 MB × 5 files at `~/.local/share/glove/audit.jsonl`
  (`%LOCALAPPDATA%\glove\` on Windows).

## Policy problems

- `glove policy validate <file>` gives schema errors with line numbers.
- A project `.glove.toml` overrides user `policy.toml` for the same key
  (scalars override, pattern lists concatenate).
- A broken policy fails the hook **closed** — fix it, nothing falls back
  to defaults silently. All failure paths with their decisions:
  [failure-modes.md](failure-modes.md).

## Signing / release artifacts

Downloads on a GitHub release ship with per-artifact `.sha256` and an
aggregated `checksums.txt`. Verify:

```bash
sha256sum -c glove-<tag>-<os>-<arch>.tar.gz.sha256
```

Signing (macOS notarization, Windows Authenticode) is applied by the
maintainer via `scripts/sign-release.sh`; unsigned artifact periods are
recorded in `RELEASE.md` §5.

## Still stuck

Open an issue with: `glove version`, `glove doctor` output, the agent +
version, and the failing command's `glove check --explain` output (never
paste secrets — check the explain output first).
