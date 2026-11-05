# Failure modes — the error-handling audit (PRD §7 0.6.0, §8)

The binding rule: **glove never returns "open" on an internal error** (PRD
§8). Every failure path below has a defined decision and a named test that
pins it — this table is checked by `TestAC_0_6_2_failure_modes_documented`
(internal/release), which fails `make check` if a cited test disappears or
the table thins out. The other half of the AC,
`TestAC_0_6_2_fuzz_fail_closed` (internal/cli), hammers corrupted inputs
through the live hook path and asserts the decision is always delivered.

## The hook decision path

| Failure | Decision | Pinned by |
|---|---|---|
| stdin is not valid JSON / truncated / empty | deny, exit 2, reason on stderr | `TestHookFailsClosedOnBrokenStdin`, `TestAC_0_6_2_fuzz_fail_closed` |
| stdin is valid JSON but not a hook payload (wrong shape, null tool_input) | deny, exit 2 (parse/missing-field error) | `TestAC_0_6_2_fuzz_fail_closed` |
| unknown agent name in `glove hook <agent>` | deny, exit 2 (Claude envelope fallback) | `TestHookFailsClosedOnUnknownAgent`, `TestAC_0_6_2_fuzz_fail_closed` |
| policy file invalid (schema error, bad mode) | deny, exit 2, key-path error on stderr — never silent defaults | `TestAC_0_3_7_invalid_policy_fails_loudly`, `TestAC_0_6_2_fuzz_fail_closed` |
| policy unreadable (permissions, missing dir) | deny, exit 2 — same fail-closed path | `TestAC_0_6_2_fuzz_fail_closed` |
| serve unreachable in standard mode | Layer 0+1 decide; undecidable allow escalates per `[general] on_model_unavailable` (review, or block); `model_available: false` in the verdict and audit | `TestAC_0_4_2_offline_flags_verdict`, `TestAC_0_4_2_offline_fallback_p95` |
| Layer 2 (laya) answers `allow` against a Layer 0/1 block | final stays block — Combine is tighten-only (§4.2) | `TestAC_0_4_3_layer2_never_downgrades`, `TestAC_0_4_3_invariant_end_to_end` |
| laya endpoint is not a laya instance / pinned digest mismatch | serve refuses to start with an actionable error | `TestAC_0_4_5_not_a_laya_refuses`, `TestAC_0_4_5_digest_mismatch_refuses` |
| non-loopback endpoint offered to serve or client | refused — the decision path is loopback-only (§3.3) | `TestAC_0_4_6_client_refuses_nonloopback`, `TestAC_0_4_6_server_binds_loopback_only` |
| hook verdict render fails | deny, exit 2 | `TestAC_0_6_2_fuzz_fail_closed` (via unknown-decision corruption) |
| audit write fails after the decision is delivered | decision stands; warning on stderr — an audit hiccup never changes a verdict | `TestAC_0_6_2_audit_write_failure_keeps_decision` |
| audit dir unwritable / disk full at rotation | decision stands; stderr warning; next successful write resumes rotation | `TestAC_0_3_4_rotation_at_10mb`, `TestAC_0_3_4_rotation_keeps_five`, `TestAC_0_6_2_audit_write_failure_keeps_decision` |
| policy state file (`policy_reload`) unreadable/corrupt | worst case: one spurious or missing reload event — the *decision* is unaffected | `TestAC_0_3_6_policy_reload_audited` |

## Install / uninstall / config management

| Failure | Decision | Pinned by |
|---|---|---|
| agent config is invalid JSON | install refuses loudly, writes nothing, no backup, original untouched | `TestAC_0_1_4_install_rejects_invalid_json`, `TestAC_0_1_4_install_preserves_config` |
| plugin path holds a foreign (non-glove) file | install refuses to overwrite it | `TestAC_0_5_1_conformance_all_adapters` (corrupt_guard phase) |
| glove shim edited by hand | `glove doctor` reports UNHEALTHY, exit 1 | `TestAC_0_5_4_doctor_detects_corrupted_config`, `TestAC_0_5_1_conformance_all_adapters` |
| cursor entry lost `failClosed: true` | doctor flags it unhealthy — that flag is the tighten-only invariant under glove faults | `TestAC_0_5_1_conformance_all_adapters`, `TestAC_0_5_4_doctor_detects_corrupted_config` |
| second install / second uninstall | no-ops, never corrupt state | `TestAC_0_1_3_install_idempotent`, `TestAC_0_5_2_kilo_install_verify_flow` |
| uninstall of a foreign file | refuses to delete | `TestAC_0_5_1_conformance_all_adapters` (uninstall phase) |
| README coverage matrix drift | `glove list --matrix --check` exits 1 with a diff | `TestAC_0_5_6_matrix_matches_readme` |
| engine misbehaves after install | install verification fails the command (exit 1), tells the user to run doctor | `TestAC_0_5_2_kilo_install_verify_flow` |

## Audit forwarding + tamper evidence (v0.7.0, ADR 4)

The sink is post-decision, opt-in, off by default: with no `[audit.sink]`
block, no dispatcher is constructed and there are zero side effects. Its
failure contract is the audit-write contract extended: a delivery can never
change a delivered verdict.

| Failure | Decision | Pinned by |
|---|---|---|
| no `[audit.sink]` in any policy | zero side effects — no file handle, no socket, no goroutine | `TestAC_0_7_5_sink_off_by_default` |
| sink target unreachable / refusing (dead webhook, down collector) | decision, exit code and verdict latency unchanged; delivery attempted once, no retry; `sink_error` event written locally | `TestAC_0_7_5_sink_failure_never_affects_verdict`, `TestAC_0_7_6_sinks_deliver_rfc5424_and_webhook` |
| sink stalls (slow collector) | the drain window (`timeout_ms`, default 500ms) bounds the post-decision wait; unconfirmed deliveries are counted as not delivered and audited via `sink_error` | `TestAC_0_7_5_sink_failure_never_affects_verdict` (slow-webhook subtest) |
| sink queue overflows | records drop, never block; the drop count is audited via `sink_dropped` | `TestAC_0_7_6_sinks_deliver_rfc5424_and_webhook` (overflow subtest) |
| malformed sink config (bad type, missing target, non-http webhook, bad protocol) | `policy validate` rejects with key path + source line; a hook treats it as invalid policy → fail closed (AC0.3.7's rule) | `TestAC_0_7_6_sink_config_validate`, `TestAC_0_3_7_invalid_policy_fails_loudly` |
| audit log edited / record deleted / file truncated | `glove audit verify` names the first broken record (edited / chain-gap / corrupt-line) and exits non-zero | `TestAC_0_7_1_verify_detects_edits_deletes_truncation` |
| pre-0.7.0 (schema-1) records in the log | verify skips them as legacy — never a false tamper alarm; the first v0.7.0 record anchors the chain | `TestAC_0_7_1_verify_detects_edits_deletes_truncation` (legacy subtest) |
| sink self-audit events themselves | written to the local log only, never forwarded — no delivery loop | `TestAC_0_7_5_sink_off_by_default` (forwarding does not duplicate locally) |
| rotation drops the oldest chain file | the first surviving record's `prev_hash` is the anchor — retention is never a false tamper alarm | `TestAC_0_7_1_chain_concurrent_and_rotation` |

## Vendor-side fail-open surfaces (documented, mitigated, not ours to fix)

| Surface | Mitigation | Documented in |
|---|---|---|
| Cursor hooks are fail-open by default | glove writes `failClosed: true`; doctor flags its absence | `docs/install/cursor.md` |
| Gemini defaults to Allow on non-JSON stdout | glove prints JSON-only stdout, logs on stderr; block rides exit 2 | `docs/install/gemini.md` |
| Codex hook error/timeout is fail-open; hosted tools bypass hooks | glove bounds itself at 30s; limits stated in the guide | `docs/install/codex.md` |
| OpenCode V2 beta does not run V1 plugins | shim targets stable V1; caveat in the guide | `docs/install/opencode.md` |
| Kilo `KILO_PURE=1` disables plugins | stated in the guide | `docs/install/kilo.md` |

## Boundaries (what is NOT a failure path)

- Undecidable gray areas in offline mode are **not** failures: they follow
  `[general] on_model_unavailable` by design (§3.2) — that is policy, not
  error handling. Pinned by `TestAC_0_4_2_offline_flags_verdict`.
- Vendor-side enforcement gaps (above) are the agents' trust models; glove
  documents them and keeps its own paths fail-closed.
