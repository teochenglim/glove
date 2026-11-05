# AGENTS.md

glove — single-binary local guard for AI agent tool calls (Go 1.27.3, `CGO_ENABLED=0`, cobra). Sits at coding agents' pre-tool-use hook point (claude-code, kilo, codex, opencode, cursor, gemini enforced; zed audit-only), decides allow / review / block *before* execution, writes an immutable audit record. The guard only ever tightens; fail-closed everywhere; zero network on the decision path.

**First move, before code:** open `RELEASE/vX.Y.Z.md` for the release being implemented — its AC table names the exact PRD bullets (`PRD-<ver>-<n>`) and test ids to build against. Red tests first.

## Where things stand (update each release)

- v0.1.0–v0.6.4 **tagged** (0.6 train = hardening + CI fixes; v0.6.4 deferred windows — no windows runner or artifact; return requires an interactive Windows pass, facts in `benchmarks/README.md`).
- **v0.7.0 SIEM-ready built** (tag pending): tamper-evident audit chain (`schema_version` 2, `glove audit verify`), MITRE ATT&CK tags on rules → records, machine identity, CEF/ECS export, opt-in off-by-default sink (file/syslog RFC 5424/webhook, ADR 4, bounded, post-decision). Audit-only fields live in the JSONL schema, `contract.Verdict` untouched.
- **v0.8.0 correlation + fleet built** (tag pending): session escalation correlation (audit-trail-derived, bounded tail scans, tighten-only — never touches deterministic verdicts; `[correlation] session_threshold`, 0 = default 3, -1 = off), `glove audit stats` (top rules/techniques/agents/decisions; per-record rule dedupe, non-rule prefixes skipped), policy bundles (`policy pack` / `policy import` — verify-before-write, `policy_import` event).
- **v0.9.0 last RC built** (tag pending): distribution (`scripts/install.sh` verify-before-extract, formula at `packaging/homebrew/` + layout contract test), failure-modes extended + hammered (`TestAC_0_9_2_*`), `CHANGELOG.md` = verbatim consolidation of RELEASE.md §4 (byte-equality contract test — regenerate CHANGELOG whenever §4 changes), sink/verify bench numbers in `benchmarks/README.md`. Semgrep lane fixed: explicit `MinVersion` on the syslog TLS dial, `**/` path patterns in vendored rules.
- Next: **v1.0.0 stability marker** (`RELEASE/v1.0.0.md`, planned — version bump only; freezes `contract.Verdict`, CLI, `policy.toml`, audit JSONL schemas; all evolution landed in 0.7.0–0.9.0). Ships ONLY after the external proofs land: two-week CI streak, signing certs, fresh-VM outside-tester pass (now including the install script + brew), 24h live soak.
- Pending the user: two-week CI streak, signing certs, fresh-VM outside-tester pass, 24h live soak, live-session smoke checks (v0.4/v0.5 agents).

## Map — never search, it's here

- **Docs:** `PRD.md` product truth (§5 interfaces, §6 CLI, §7 roadmap + acceptance bullets, §8 NFRs) · `ROADMAP.md` order/reasons · `RELEASE.md` roll-up + changelog + verified/unverified ledger (§5) · `RELEASE/vX.Y.Z.md` per release · `CHANGELOG.md` (verbatim consolidation of RELEASE.md §4 — regenerate it whenever §4 changes; byte-equality test gates drift) · `docs/adr/` decisions · `docs/install/<agent>.md` ×7 · `docs/failure-modes.md` (error-handling audit, contract-checked) · `docs/siem.md` (ingestion recipes, v0.7.0) · `docs/troubleshooting.md` · `docs/policy-cookbook.md` · `docs/adapter-authoring.md` · `benchmarks/README.md` + `soak.sh` · `packaging/homebrew/` (formula + per-release sha256 fill via `scripts/update-homebrew-sha.sh`).
- **Code:** `cmd/glove/main.go` → `internal/cli` (hook/check/install/uninstall/list/doctor/audit/policy/serve; `RunHook` fail-closed, exit 2 = block, audit written after the decision) · `internal/decision` (engine + Layer 0, `chain.go` = Layer 1, `verdict.go` Combine arithmetic, `laya.go` serve client) · `internal/rules` (embedded `builtin.yaml`, `PathHits` normalizes `~`/`$HOME`/`%USERPROFILE%`/`%LOCALAPPDATA%`) · `internal/policy` (TOML, project > user > builtin, line-numbered validate) · `internal/audit` (§5.4 record — schema v2: hash chain + machine identity + techniques; writer stamps+returns & rotates 10 MB × 5, `verify.go` tamper walk, `sink.go` ADR 4 dispatcher file/syslog/webhook, `siem.go` CEF/ECS, `correlate.go` bounded session-tail scan, `stats.go` aggregation, reader query + `--technique`, `identity.go`, redact) · `internal/adapter` (frozen §5.1 interface + registry, per-agent files, OpenCode-lineage TS shims + goldens + conformance suite) · `internal/server` (loopback daemon, `laya_runtime.go` ADR 3) · `internal/paths` (OS dirs, GLOVE_DATA_DIR/GLOVE_CONFIG_DIR) · `internal/release` (CI/release config-as-contract tests) · `pkg/contract` (frozen §5.3 Verdict + freeze tests) · `testdata/` fixtures · `scripts/` (sign-release, ac-map, release-check, pin-actions).

## Commands

- `make check` (fmt-check, vet, ac-map, test, version-check) — **must be green before claiming anything works; CI runs exactly this.** CI is deterministic-only.
- `make stress` (GLOVE_STRESS=1) — §8 timing budgets, 10k query, soak, 5-target cross-compile, artifact roundtrip. Run before calling a release ready; also `make race`, `make audit` (govulncheck), `scripts/release-check.sh X.Y.Z`, `make github-action-bump`.
- `VERSION` ↔ `internal/version` via `make version-check`; `make bump` rewrites both. **Never run `make bump` / `make release` — user-only** (git rule below).

## Hard rules

- **The user does ALL git commits/pushes/tags/releases.** The agent never runs git commit/push/tag, `make bump`, `make release`. Finish by handing over a one-line commit message. One branch per version (`release/X.Y.Z`); no 0.X merge until its predecessor is tagged.
- **ACs before code:** write `RELEASE/vX.Y.Z.md` ACs first, red tests first. Tests: `func TestAC_<M>_<m>_<n>_<slug>`, ≥ 2 per AC (`t.Run("ac_<M>_<m>_<n>_<slug>",…)` counts). Status `planned` = no tests counted yet. AC tests are the demo — no browser pass (no UI); report what was *not* exercised honestly in Built + `RELEASE.md` §5. Scratch files never enter the repo.
- **Only ever tightens** (ADR 1): block > review > allow across layers and chain segments; Layer 2 never downgrades Layer 0/1 — the invariant test in `internal/decision` is load-bearing.
- **Fail closed:** internal error → delivered deny + exit 2, never crash-open; exit 0 allow/review, 2 block; every other exit is a failure.
- **`pkg/contract` frozen** (§5.3, ADR 2): no field/tag/enum change without a schema-version bump + owner sign-off; the freeze test is the tripwire. Adapters and serve depend only on this package.
- **No `time.Now()` inside `internal/decision`** — take `cfg.Now`; wall clock only at the CLI/server/audit boundary (semgrep rule).
- **No audit plaintext by default:** `input_hash` (SHA-256) always; raw input only behind `--audit-verbose` with redaction.
- **Static binary:** CGO_ENABLED=0, `-trimpath`, version via `-ldflags -X`. No Node/bundler/UI; embedded assets only via `//go:embed`. New dep = a row in `RELEASE.md` §1 + a line in the release file; watch the ≤ 15 MB stripped budget when pulling heavy deps, early.
- **Changed behaviour** → PRD section + release file + `RELEASE.md` (§2/§4/§5) updated in the same change; new decision → `docs/adr/NNNN-*.md`.
- **CLI subcommands are frozen surface (§6)** — copy names verbatim from day one; a rename after 1.0.0 is breaking.
- **The guard never executes agent-proposed commands** — vendored semgrep rule `go-command-injection` enforces this; scope it, never delete it.
- **Windows is deferred** (2026-11-04, PRD §8): never add a windows runner or artifact without an interactive Windows test pass; keep `GOOS=windows go vet ./...` clean.
- **Outbound sink (0.7.0, ADR 4):** post-decision, opt-in, off by default, one delivery attempt, bounded queue; delivery never affects a verdict and self-audit events are never forwarded (no loop). Change any of that and ADR 4 changes first.
- The agent cannot run `/compact` — tell the user to.

## Pitfalls already paid for

- Makefile: `VAR := value # comment` keeps trailing spaces in the value — comments on their own line.
- macOS: `sed -i ''` or `perl -pi -e`; verify a scripted replacement landed.
- `x, err := f()` inside a loop shadows the outer err — use `var err error; x, err = …` when accumulating; grep for it in review.
- `filepath.ToSlash` is a no-op on Unix — normalize path separators string-level.
- Shared runners flake wall-clock budgets → timing tests live in `make stress`; CI gates use the 50ms absorb-noise form where a gate remains.
- Platform-shaped test fixtures (release artifacts, paths, binaries) must derive the platform from `runtime` (aliased `guntime` here — `internal/cli` declares its own `runtime` struct), never hardcode the dev machine's — the v0.9.1 fixture 404'd on ubuntu for exactly that.
- Laya judges `git status` as system-reaching → review: honest opinion, don't "fix" calibration (blast radius never blocks alone); point users at `mode = "fast"`. Adapter vendor fail-open traps (cursor `failClosed: true`, gemini non-JSON stdout, codex hook timeout, `KILO_PURE=1`, opencode V2 beta) — `docs/failure-modes.md` is the source.
