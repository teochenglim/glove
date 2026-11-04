# Release plan

Roll-up and changelog: [`../RELEASE.md`](../RELEASE.md). One file per release here: scope, acceptance criteria (each mapped to named tests), notes, checklist.

| Release | Theme | Exit gate (summary) | Depends on |
|---|---|---|---|
| [v0.1.0](v0.1.0.md) | Claude Code + Layer 0 | 5 ACs pass in CI (macOS + Linux); live `rm -rf /` → block smoke check recorded; tagged | - |
| v0.2.0 | Layer 1: chain splitting | 50-case chain fixture ≥ 95% segment accuracy; `--explain` per-segment verdicts; ≤ 20ms P95 | 0.1.0 tagged |
| v0.3.0 | Audit log | one record per hook; zero plaintext leakage on a 20-command fixture; rotation at 10 MB; 10k-record query ≤ 200ms | 0.2.0 tagged |
| v0.4.0 | Layer 2: Laya + `serve` | P95 ≤ 150ms; invariant test (Layer 0 block beats Laya allow); adversarial benchmark ≥ 90% block-hit; offline fallback works | 0.3.0 tagged |
| v0.5.0 | Kilocode adapter | install ≤ 2 min per docs; shim tsc-clean; adapter interface unchanged from 0.1.0 | 0.4.0 tagged |
| v0.6.0 | Codex + OpenCode | all three agents pass the conformance suite; `glove doctor` catches a corrupted hook config | 0.5.0 tagged |
| v0.7.0 | Policy engine | project > user > built-in precedence tested; invalid policy fails loudly; `--fast` ≤ 20ms P95 | 0.6.0 tagged |
| v0.8.0 | Cursor, Gemini CLI, Zed | every agent has a working install+verify flow; Zed honestly `mode: audit`; README matrix machine-verified | 0.7.0 tagged |
| v0.9.0 | Release candidate: hardening | all-platform CI green 2 weeks; zero P0/P1; signed binaries + checksums; 24h soak clean | 0.8.0 tagged |
| v1.0.0 | Production ready | bit-identical behaviour to last 0.9.x RC except the version string (conformance + benchmark re-run) | 0.9.0 tagged |

## Versioning

SemVer. `0.x` minors map one-to-one to the PRD §7 roadmap; patches are fixes with no AC change and get a short `v0.x.y.md`. No `0.X.0` branch merges until its predecessor is tagged (PRD §10.2). `1.0.0` is purely a stability marker: it freezes the `contract.Verdict` schema (§5.3), the CLI surface (§6), the `policy.toml` schema (§5.5) and the audit JSONL schema (§5.4); a consolidated `CHANGELOG.md` is generated from `RELEASE.md` §4 at that point.
