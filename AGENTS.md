# AGENTS.md

glove: a single-binary local guard for AI agent tool calls — sits at the pre-tool-use hook point of coding agents (claude-code, kilo, codex, opencode, cursor, gemini enforced; zed audit-only), decides allow / review / block *before* execution, writes an immutable audit record. One Go 1.27.3 static binary (`CGO_ENABLED=0`, cobra CLI). No external service, zero network on the decision path; the guard only ever tightens. Product truth: `PRD.md` (§5 frozen interfaces, §6 CLI surface, §7 roadmap with acceptance bullets, §10 kickoff discipline); plan: `RELEASE.md` + `RELEASE/v*.md`; order: `ROADMAP.md`; decisions: `docs/adr/`.

**First move, before touching code:** open `RELEASE/vX.Y.Z.md` for the release you're implementing. Its AC table names the exact PRD bullets (`PRD-<ver>-<n>`) and test identifiers to write code against — read it before the spec, before the handler, before anything else below.

## Where things stand (update this block each release)

- **Built, `ready` (v0.1.0–v0.3.0 tagged; v0.4.0 + v0.5.0 + v0.6.0 untagged):** Layer 0 + 1 + audit + policy + Layer 2 + the adapter matrix + hardening — seven agents (six enforcement, zed audit-only), CI platform matrix (windows/amd64 = build+vet+short tests; full perf gates stay on POSIX runners), release pipeline for all five §8 platforms with verified checksums + scripted signing, the error-handling audit (`docs/failure-modes.md`, contract-checked by `internal/release`), fuzz-proven fail-closed hooks, Windows path normalization, `benchmarks/` + soak at three levels. External proofs pending the user: two-week CI streak (starts at push), signing certificates, fresh-VM outside-tester pass, 24h live soak, and the v0.4.0/v0.5.0 live-session smoke checks.
- **Next:** v1.0.0 (stability marker — version bump only; freezes `contract.Verdict`, CLI surface, `policy.toml` schema, audit JSONL schema; consolidated `CHANGELOG.md` from `RELEASE.md` §4). Ship after the 0.6.0 external proofs land.
- **Laya pitfall:** the container judges `git status` as system-reaching → review; that is the checkpoint's honest opinion — don't 'fix' it in calibration (blast radius must never block alone), point users at `mode = "fast"`.
- **Adapter pitfalls:** vendor fail-open defaults are the enemy — cursor hooks need `failClosed: true` (doctor flags its absence as UNHEALTHY), gemini fails open on non-JSON stdout (glove prints JSON-only stdout, logs to stderr), codex fails open on hook timeout (glove bounds itself at 30s; hosted tools bypass codex hooks entirely). OpenCode V2 beta breaks V1 plugins — the shim targets stable V1. Kilo: `@kilocode/plugin` + `export default { id, server }`; OpenCode: any exported function + `@opencode-ai/plugin`; both shim `(input, output)` with mutate-`output.args`-in-place semantics. `KILO_PURE=1` disables Kilo plugins.
- **Docs:** `RELEASE.md` roll-up (status table, dependency list, changelog newest first, §5 = verified/unverified ledger) · `RELEASE/vX.Y.Z.md` per release (status, ACs with named tests, built/limits) · `ROADMAP.md` · `PRD.md` · `docs/adr/` · `docs/install/` (one guide per agent: claude-code, kilo, codex, opencode, cursor, gemini, zed) · `docs/adapter-authoring.md` · `docs/failure-modes.md` (the error-handling audit — contract-checked: cited tests must exist) · `docs/troubleshooting.md` · `docs/policy-cookbook.md` · `benchmarks/README.md` (repro instructions for every §8 claim; `soak.sh` mechanical soak) · `README.md` (coverage matrix = generated block, `--check`-verified).
- **Code map:** `cmd/glove/main.go` (thin wiring: cobra → `internal/cli`) · `internal/cli/` (`root` = loadRuntime: policy → engine; `check`/`hook`/`install`/`uninstall`/`list` (+`--matrix --check`)/`doctor`/`audit`/`policy validate`/`serve`; `RunHook` fail-closed deny + exit 2 on internal errors, deny rendered in the resolved agent's envelope, consults serve in standard mode, audit written after the decision is delivered; timing budgets scale under `-race` via `timing_race.go`/`timing_normal.go`) · `internal/release/` (config-as-contract tests: CI matrix, release pipeline, sha256 roundtrip, failure-modes audit doc) · `internal/server/` (`server.go` loopback-only daemon + /evaluate, `laya_runtime.go` the laya container adapter: probe/setup/ask/calibrate — ADR 3) · `internal/audit/` (`schema.go` §5.4 record, `writer.go` 10 MB × 5 rotation, `reader.go` query/tail, `redact.go` verbose-only redaction) · `internal/policy/` (§5.5 schema, project>user>builtin merge — scalars override, lists concatenate — validate with line numbers, `Hash()` drives reload detection) · `internal/decision/` (`engine.go` dispatch + Layer 0, `chain.go` Layer 1 splitter + composition pairs, `verdict.go` Combine = §4.2 arithmetic, `laya.go` the serve client — loopback-only) · `internal/rules/` (`builtin.yaml` embedded + `Compose` policy extensions; `PathHits` normalizes `~`/`$HOME`/`%USERPROFILE%`/`%LOCALAPPDATA%` and both path separators — expansion only adds candidates) · `internal/adapter/` (`adapter.go` frozen §5.1 interface + registry + `AgentStatus`/`StatusReporter` optional health layer; `configjson.go` shared install/uninstall/status for JSON configs; `hookwire.go` per-agent wire shapes; `shim.go` + `templates/shim.ts.tmpl` the OpenCode-lineage plugin shims; `claude.go`, `kilo.go`, `codex.go`, `opencode.go`, `cursor.go`, `gemini.go`, `zed.go`; conformance harness + shim goldens + tsc gate in the tests) · `internal/paths/` (data/config/audit/policy + per-agent config paths, GLOVE_DATA_DIR/GLOVE_CONFIG_DIR overrides) · `internal/version/` · `pkg/contract/` (**frozen §5.3** + shape-freeze tests) · `testdata/` (hook + chain fixtures, `conformance/` = per-agent stdin fixtures + tsc-validated shim goldens) · `benchmarks/` (repro docs + `soak.sh`) · `scripts/sign-release.sh` (maintainer-run signing).

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
