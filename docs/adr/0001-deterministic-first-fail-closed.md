# ADR 1: Deterministic-first, fail-closed decision path

**Status:** accepted (given by the PRD — recorded here because every layer of the code depends on it)

**Context.** Glove decides allow / review / block for agent tool calls at hook frequency. Regex alone cannot distinguish `cat ~/.ssh/id_rsa` from `cat ~/.ssh/config`, has no confidence, and breaks on composition (PRD §2.2). A model-first design would put latency and uncertainty on deterministic catastrophes.

**Decision.**

1. Three layers, evaluated in order, worst-part-wins: Layer 0 deterministic rules (zero latency) → Layer 1 shell-aware chain splitting → Layer 2 Laya model for gray areas only (PRD §4.1).
2. **The guard only ever tightens**: `final = block if any layer returned block, else review if any returned review, else allow` (PRD §4.2). Layer 2 can never downgrade a Layer 0/1 block. Enforced in `internal/decision/engine.go` with a unit test that feeds a Layer 0 `block` through a mocked Laya `allow`.
3. **Fail closed, fail usable**: if `serve` is unreachable, `hook` degrades to Layer 0 + 1 and undecidable calls return `review` (or `block` per policy) — the guard never silently becomes an open gate (PRD §3.2). Internal errors are never returned as "allow" (PRD §8).
4. **Zero network on the decision path** (PRD §3.3): no cloud, no telemetry, model inference local only; verified in CI from 0.4.0 (`strace`/`dtruss`).
5. **The guard never executes** what it inspects: Glove reads `tool_name` + `tool_input` and decides; it never runs agent-proposed commands. Enforced by the vendored semgrep rule `go-command-injection`.

**Consequences.** Deterministic catastrophes cost zero latency and zero uncertainty; calibration effort concentrates on the gray areas where "review" is an honest answer. The invariant test and the semgrep rule are load-bearing — deleting either is a spec change, not a cleanup.

**Alternatives considered.** Regex-only (rejected: PRD §2.2 — shape ≠ semantics, no confidence, breaks on composition); model-first (rejected: uncertainty and latency on the deterministic 5% that matters most); cloud decision service (rejected: PRD §3.3 non-goal — zero network, local only).
