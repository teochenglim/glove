# glove benchmark suite

Reproducibility instructions for every performance claim in the release
files (PRD §7 0.6.0: benchmark suite published with reproducibility
instructions). All gates live in CI via `make check`; the commands below
reproduce them locally.

Prereqs: Go 1.27.3, `make build` → `./bin/glove`. No external services —
pin a dead serve endpoint (`GLOVE_SERVE_ENDPOINT=http://127.0.0.1:1`) so
benches never measure leftover model compute.

## 1. Hook cold start (PRD §8: ≤ 15ms median)

True cold start: full process spawn through the hook path (stdin parse →
Layer 0+1 → render), 2 discarded warmups absorb first-exec artifacts.

```bash
GLOVE_SERVE_ENDPOINT=http://127.0.0.1:1 ./bin/glove doctor --bench hook
# hook cold-start over 20 runs: min …  median …  p95 …  max …
# budget: 15.0ms (median) — PASS
```

Measured (2026-11-04, MacBook Pro M1, macOS arm64, 40 runs): **min 7.5ms /
median 8.0ms / p95 9.1ms**. This is the fork-overhead measurement for §8 —
the dominant cost is process spawn + Go runtime init, not the decision
(engine evaluation is sub-millisecond). Windows numbers: pending the
Windows CI runner / maintainer hardware (the same command works there;
`go test -short` keeps the bench off Windows CI to avoid spawn-latency
flakes). CI gate: `TestAC_0_1_5_hook_cold_start_budget`.

Compound-payload variant (Layer 1 worst case, §7 0.2.0 budget 20ms P95):
`TestAC_0_2_4_hook_p95_compound_payload` (subprocess gate) and
`TestAC_0_2_4_chain_latency_budget` (in-process tripwire).

## 2. Serve-path overhead (PRD §8: ≤ 150ms P95, model compute excluded)

100-call fixture through hook → serve → calibration with the stub runtime:

```
go test ./internal/cli/ -run 'TestAC_0_4_1' -count=1 -v
```

Real-model wall time (~0.5–2s warm per four-question call on CPU) is model
compute, deliberately outside this gate — see ADR 3.

## 3. Offline fallback (PRD §8: ≤ 20ms; mechanism gate ≤ 5ms P95)

```
go test ./internal/decision/ ./internal/cli/ -run 'TestAC_0_4_2|TestAC_0_3_8' -count=1 -v
```

## 4. Adversarial block rate (§7 0.4.0: ≥ 90% on the deterministic layers)

```
go test ./internal/decision/ -run 'TestAC_0_4_4' -count=1 -v
```

Corpus: `testdata/adversarial_benchmark.jsonl` (100 samples). The semantic
(model-in-the-loop) half needs a live laya container — future work,
recorded in `RELEASE/v0.4.0.md`.

## 5. Audit query under load (§7 0.3.0: 10k records ≤ 200ms)

```
go test ./internal/cli/ -run 'TestAC_0_3_2_query_10k' -count=1 -v
```

## 6. Soak (§7 0.6.0: no P0 in 24h — accelerated proxy here)

Accelerated soak as CI tests: 2,000 mixed in-process decisions + 50 real
subprocess hooks with audit integrity checks:

```
go test ./internal/cli/ -run 'TestAC_0_6_5' -count=1 -v
```

Longer mechanical soak (between the accelerated gate and a real 24h live
session), ~30–60 min of continuous hook processes:

```bash
./benchmarks/soak.sh [iterations]   # default 500; exit non-zero on any deviation
```

The true 24h soak in live Claude Code + Kilo sessions is a human gate —
run glove installed for a working day and record the result in
`RELEASE.md` §5.

## Timing notes

- **The load- and timing-heavy gates run via `make stress` (GLOVE_STRESS=1),
  not in CI** — shared runners turn wall-clock budgets into flakes: the
  10k-query gate measured 335ms on a 2-vCPU macos runner that a laptop
  passes at ~50ms, and windows runner spawn measured **57.1ms median**
  cold start (2026-11-04) against the 15ms budget — a platform fact, not a
  regression. `make stress` on release hardware is where the §8 budgets
  are verified; run it before tagging (AGENTS.md checklist).
- Windows has no timing gate by design (the CI windows job runs
  `go test -short`); its cold-start number above is informational.
- Run benches on an otherwise idle machine; the median is the gate.

## v0.9.0 — sink + verify overhead (off the decision path)

Measured 2026-11-05, local macOS arm64, Go 1.27.3, otherwise idle
(`make stress` asserts the budgets via
`TestAC_0_9_3_sink_verify_overhead_budgeted`; the decision-path §8 budgets
are unaffected by construction — the hook dispatches the sink strictly
after the verdict is rendered, and `audit verify` is an offline CLI
command):

| Measurement | Result | Budget |
|---|---|---|
| `audit verify` over 10,000 chained records (re-hash + linkage walk) | **~0.50s** wall | < 1s |
| file sink: 50 deliveries + drain (enqueue → confirmed delivered) | **< 1ms** | < 50ms |
| syslog (udp, loopback) delivery + drain | **~50ms** (dominated by the deliberate 50ms ICMP port-unreachable read window) | < 100ms |
| webhook delivery | per-attempt client timeout 2s; drain window bounds the hook's wait (`timeout_ms`, default 500ms) | bounded by design |

Reading: the decision path pays nothing for the sink or verify — the
`latency_ms` in an audit record is stamped before the sink is consulted,
and verify never runs on the hook path. A dead or stalling collector
extends only the post-decision drain window, never the §8 budgets.
