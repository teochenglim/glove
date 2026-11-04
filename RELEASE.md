# glove release log and summary

The single place to see where glove stands: what is shipped, what is planned, what each release proves, and how a release is cut. Per-release detail (scope, acceptance criteria, named tests, checklist) lives in [`RELEASE/`](RELEASE/README.md); this file is the readable roll-up and the changelog.

> **Current state:** v0.1.0 ready (implemented and locally verified; live Claude Code smoke check + tag pending the user). Statuses below are updated when a release changes state, in the same change as the code.

## 1. Summary

glove is a single-binary local guard for AI agent tool calls. Installed at the `PreToolUse` hook point of an agent (Claude Code first), it intercepts every tool invocation before execution, makes a three-way decision — allow / review / block — and writes an immutable audit record. The engine is layered and the invariant that separates it from regex guardrails is: **the guard only ever tightens** — deterministic rules decide the catastrophes at zero latency, and the model can never downgrade them.

| Fact | Value |
|---|---|
| Language / runtime | Go 1.27.3 (`go.mod`, CI `GO_VERSION`, `golang:1.27-alpine` builder), single static binary `CGO_ENABLED=0` |
| Runtime external dependency | none. Zero network calls on the decision path (PRD §3.3). The Laya GGUF model (~644 MB) is downloaded once at first `serve` with SHA-256 verification (v0.4.0) |
| Releases planned | 10 (v0.1.0 → v1.0.0, PRD §7) |
| Requirements / ACs | PRD §7 acceptance bullets are the requirements (ids auto-derived as `PRD-<ver>-<n>` by `make ac-map`); v0.1.0: 5 ACs, each governed by ≥ 2 named tests |
| Licence | Apache-2.0 |
| Artifacts per tag | tarballs for linux and macOS on amd64 and arm64 (+ `.sha256`), multi-arch image on GHCR, GitHub Release with notes from `RELEASE/<tag>.md`. Windows artifacts join the matrix at v0.9.0 (PRD §8) |

### Locked dependency list

New dependency = a row here (pinned as resolved by `go get <pkg>@latest`) + a line in the release file that introduced it.

| Dependency | Version | Locked where | Since |
|---|---|---|---|
| github.com/spf13/cobra | v1.10.2 | go.mod / go.sum | scaffold (CLI framework, PRD §10.1) |
| gopkg.in/yaml.v3 | v3.0.1 | go.mod / go.sum | v0.1.0 (embedded builtin.yaml rules) |
| github.com/google/uuid | v1.6.0 | go.mod / go.sum | v0.1.0 (audit IDs) |
| github.com/inconshreveable/mousetrap | v1.1.0 | go.sum (cobra dep, windows) | scaffold |
| github.com/spf13/pflag | v1.0.9 | go.sum (cobra dep) | scaffold |

## 2. Release status

Newest first.

| Release | Theme | Status | ACs | Requirements covered |
|---|---|---|---|---|
| [v0.1.0](RELEASE/v0.1.0.md) | Claude Code + Layer 0 | ready | 5 (10 tests) | 5/5 (PRD-0.1.0-1…5) |
| v0.2.0 | Layer 1: chain splitting | planned | - | - |
| v0.3.0 | Audit log | planned | - | - |
| v0.4.0 | Layer 2: Laya + `serve` | planned | - | - |
| v0.5.0 | Kilocode adapter | planned | - | - |
| v0.6.0 | Codex + OpenCode | planned | - | - |
| v0.7.0 | Policy engine | planned | - | - |
| v0.8.0 | Cursor, Gemini CLI, Zed | planned | - | - |
| v0.9.0 | Release candidate: hardening | planned | - | - |
| v1.0.0 | Production ready (stability marker) | planned | - | - |

## 3. How a release is cut

1. Write/refresh `RELEASE/vX.Y.Z.md` ACs **before** code (red tests first). One branch per version: `release/0.1.0`, `release/0.2.0`, … (PRD §10.2). No `0.X.0` merges until its predecessor is tagged.
2. Implement until `make check` is green — fmt-check, vet, ac-map, tests, version-check (CI runs exactly this). Add `make audit` before calling a release ready.
3. Tick the checklist; update this file (§2/§4/§5), `ROADMAP.md`, `AGENTS.md` and any ADR touched, in the same change.
4. Agent hands over a one-line commit message. **User** commits on `release/X.Y.Z`, then runs `make release VERSION=x.y.z`.
5. CI: gate (`make check`, tag == `VERSION`) → cross-compile build (linux/mac × amd64/arm64, `CGO_ENABLED=0`) → GitHub Release (notes = `RELEASE/<tag>.md`) + multi-arch image on GHCR.

## 4. Changelog

Newest first. One paragraph per release: what shipped, what it proves.

### v0.1.0 — Claude Code + Layer 0 (ready, untagged)

Layer 0 shipped end to end: `glove check` for manual verdicts, `glove hook claude-code` for the live PreToolUse path (exit 0/2 per §6, fail-closed on internal errors), `glove install claude-code` (diff + backup + idempotency + two verification test calls), and `glove doctor --bench hook` enforcing the 15ms cold-start budget (measured 4.7ms median locally). The builtin rule set (Appendix B: 5 hard-block, 4 force-confirm, 5 protected-path globs) is embedded and compiled at startup. This proves the deterministic layer and the adapter abstraction hold before anything smarter lands.

## 5. Verified and unverified

| Area | Verified by | Not exercised / limits |
|---|---|---|
| Scaffold (2026-11-04) | `go build ./...`, `go test ./...` (contract freeze tests), `glove version` → `0.1.0`, `make check` green locally | Nothing in PRD §7 existed yet at scaffold time |
| v0.1.0 engine + CLI (2026-11-04, local macOS arm64) | `make check` + `make race` green; 10-case hook fixture set; AC tests 2×5; CLI transcript (block/allow/review, protected paths, PRD §2.2 ssh-config distinction); hook exit codes 0/2 + fail-closed on broken stdin; install flow in a throwaway HOME (diff, backup, idempotency, broken-JSON guard); `doctor --bench hook` median 4.7ms vs 15ms budget | **Live Claude Code session not exercised** (needs install on the user's machine — run before tagging); CI never ran on a tag; linux/windows builds unexercised; govulncheck not installed locally |
