# AGENTS.md

glove: a single-binary local guard for AI agent tool calls — sits at the `PreToolUse` hook point (Claude Code first, later Kilocode/Codex/OpenCode/Cursor/Gemini/Zed), decides allow / review / block *before* execution, writes an immutable audit record. One Go 1.27.3 static binary (`CGO_ENABLED=0`, cobra CLI). No external service, zero network on the decision path; the guard only ever tightens. Product truth: `PRD.md` (§5 frozen interfaces, §6 CLI surface, §7 roadmap with acceptance bullets, §10 kickoff discipline); plan: `RELEASE.md` + `RELEASE/v*.md`; order: `ROADMAP.md`; decisions: `docs/adr/`.

**First move, before touching code:** open `RELEASE/vX.Y.Z.md` for the release you're implementing. Its AC table names the exact PRD bullets (`PRD-<ver>-<n>`) and test identifiers to write code against — read it before the spec, before the handler, before anything else below.

## Where things stand (update this block each release)

- **Built, `ready` (v0.1.0 tagged; v0.2.0 untagged):** v0.1.0 Claude Code + Layer 0 and v0.2.0 Layer 1 chain splitting — `check`/`hook`/`install`/`doctor`, composition rules, 50-case chain fixture at 100%. Live Claude Code session smoke check still pending the user; run it before cutting the v0.2.0 tag.
- **Next:** v0.3.0 (audit + policy — the PRD §7 roadmap was consolidated 2026-11-04: old 0.3.0–0.9.0 merged into 0.3.0/0.4.0/0.5.0/0.6.0), see `RELEASE/v0.3.0.md`.
- **Docs:** `RELEASE.md` roll-up (status table, dependency list, changelog newest first, §5 = verified/unverified ledger) · `RELEASE/vX.Y.Z.md` per release (status, ACs with named tests, built/limits) · `ROADMAP.md` · `PRD.md` · `docs/adr/` · `docs/install/claude-code.md` · `README.md`.
- **Code map:** `cmd/glove/main.go` (thin wiring: cobra → `internal/cli`) · `internal/cli/` (`root/check/hook/install/doctor`; `RunHook` is the PreToolUse path — fail-closed deny + exit 2 on internal errors; audit/policy/list/serve subcommands land with their releases) · `internal/decision/` (`engine.go` dispatch + Layer 0, `chain.go` Layer 1 splitter + composition pairs, `verdict.go` Combine = §4.2 arithmetic; L2 `laya.go` v0.4.0) · `internal/rules/` (`builtin.yaml` embedded: hard/force/composition/protected, glob→regex paths) · `internal/adapter/` (`adapter.go` interface §5.1, `claude.go` install/diff/backup + render/parse) · `internal/paths/` (home/claude settings) · `internal/version/` · `pkg/contract/` (**frozen §5.3** + shape-freeze tests) · `testdata/` (`claude_hook_cases.json`, `chain_split_cases.json`; audit fixtures land v0.3.0).

## Commands

- `make check` (= fmt-check, vet, ac-map, test, version-check): must be green before claiming anything works. CI runs exactly this.
- `make build` → `./bin/glove` (static, `-trimpath`, version injected) · `make run` · `make race` · `make clean`.
- `make ac-map` — traceability gate: every PRD §7 acceptance bullet (`PRD-<ver>-<n>`) → AC row → ≥ 2 named tests. `scripts/release-check.sh X.Y.Z` verifies one release.
- `make audit` (govulncheck) before calling a release ready; `make github-action-bump` refreshes pinned action SHAs.
- AC tests are package-local Go tests named `func TestAC_<M>_<m>_<n>_<slug>` (≥ 2 per AC, `t.Run("ac_<M>_<m>_<n>_<slug>", …)` allowed inside a parent carrying the same prefix). Fixtures live in `testdata/`. Status `planned` = no tests counted yet; leave `planned` until the release checklist ticks.
- **Before declaring a release ready:** `make check`; `make audit`; the live-agent smoke check from the release file performed and recorded in `RELEASE.md` §5; docs updated. There is no browser pass — glove has no UI (PRD §4.3); a CLI transcript is the demo.

## Git & release (important)

- **The user does ALL git commits, pushes, tags and releases. The agent never runs git commit/push/tag, `make bump` or `make release`.** Finish by handing over a one-line commit message; the user commits, then releases.
- One branch per version (PRD §10.2): `release/0.1.0`, `release/0.2.0`, … No `0.X.0` merges until its predecessor is tagged. `make release` amends HEAD and tags it, so it needs ≥ 1 commit and an `origin` remote.
- `VERSION` is the source of truth; `internal/version/version.go` mirrors it (`make version-check` enforces; `make bump` rewrites both). `glove version` prints the bare semver (PRD §10.5).
- The agent cannot run `/compact`; tell the user to.

## Rules

- **Static binary:** `CGO_ENABLED=0`, `-trimpath`, version via `-ldflags -X`. No libc/OpenSSL surface; the ≤ 15 MB stripped size budget (§8) is a warning-level gate — check it when pulling heavy deps, early, not at the end.
- **The guard never executes** agent-proposed commands — it inspects and decides. The vendored semgrep rule `go-command-injection` enforces this; scope it, never delete it.
- **Only ever tightens** (ADR 1): combination is `block > review > allow` across layers and across chain segments; Layer 2 can never downgrade Layer 0/1. The invariant test in `internal/decision` is load-bearing.
- **Fail closed:** serve unreachable → Layer 0+1, undecidable = `review` (or `block` per policy); hook exits 0 (allow/review), 2 (block); every other exit is a failure — never crash-open (§6, §8).
- **`pkg/contract` is frozen** (§5.3, ADR 2): no field/tag/enum change without a schema-version bump and owner sign-off; the freeze test is the tripwire. Adapters and serve depend only on this package.
- **Injected clock:** no `time.Now()` inside `internal/decision` — take `cfg.Now`; wall-clock reads live at the CLI/server/audit boundary only (semgrep `go-time-now-in-decision`).
- **Shadowing trap:** `x, err := f()` inside a loop shadows the outer `err`; use `var err error; x, err = …` when accumulating.
- **No audit log plaintext by default** (§7 0.3.0): `input_hash` (SHA-256) always; raw input only behind `--audit-verbose` with redaction.
- **Spec first (n/a):** no OpenAPI here — `pkg/contract` is the spec (ADR 2). Don't add `api/` without revisiting ADR 2.
- **Dependencies:** new dep = a row in the locked list (`RELEASE.md` §1) + a line in the release file. `go get <pkg>@latest`, then pin the resolved version. Run heavy-dep builds early to watch binary size.
- **Version:** `VERSION` ↔ `internal/version` (`make version-check` / `make bump`). `glove version` prints the bare semver.
- Changed behaviour = update the PRD section, the release file and `RELEASE.md` (§2/§4/§5) in the same change. New decision = `docs/adr/NNNN-*.md`.
- No Node, no bundler, no UI. If embedded assets ever become real, they come in via `//go:embed` and the binary is rebuilt.

## Working method that worked

1. Read `RELEASE/vX.Y.Z.md` first — the AC table names the test identifiers; write code aimed at them, red tests first.
2. Engine/rules → adapters → CLI wiring → tests (fixtures in `testdata/`) → the live-agent smoke check from the release file → docs → full gate (`make check`, `make audit`).
3. AC tests are the demo — skip a separate playground. Report honestly what was not exercised (live agent session? latency on target hardware?) in the release file's Built section and `RELEASE.md` §5.
4. Scratch files go in the session scratchpad, never the repo.
5. Latency budgets (§8) are CI gates once `glove doctor --bench hook` exists (0.1.0); measure on the target hardware before claiming them.

## Pitfalls already paid for

- Makefile: `VAR := value # comment` keeps the trailing spaces in the value; put comments on their own line.
- macOS: `sed -i ''` or `perl -pi -e`; check a scripted replacement landed (formatters reflow code).
- Go shadowing (above) — the loop-`:=` form compiles fine and silently drops the outer error; grep for it in review.
- cobra: subcommand names are frozen CLI surface (§6) — renaming one after 1.0.0 is a breaking change, so pick names from §6 verbatim from day one.

## Supply chain

`.github/workflows/security.yml` (vendored Semgrep rules, Trivy fs + image, SARIF, weekly, actions pinned to full SHAs via `make github-action-bump`). CI runs `govulncheck` (`ci.yml` supply-chain job). Runtime image is distroless, non-root. `govulncheck` before release: `make audit`.
