# ADR 4: Audit forwarding is post-decision, opt-in, and off by default

## Context

PRD §3.3 declares two non-goals that the 0.7.0 SIEM arc touches: "not a cloud
service" and "zero network calls on the decision path." SIEM ingestion needs
records to leave the machine (syslog collector, HTTP collector, or a plain
file another agent tails), which sounds like it collides with both. It does
not — provided the boundary is drawn at the right place: the *decision*, not
the process lifetime.

## Decision

1. **The invariant governs the decision, not the process.** "Zero network on
   the decision path" (§3.3, §8 strace gate) means: nothing on the path from
   stdin read → Layer 0/1/2 evaluation → verdict rendering reads or waits on
   a non-loopback network. Sink delivery happens strictly *after* the verdict
   bytes are on stdout and the exit code is fixed — it can inform nothing and
   change nothing.
2. **Off by default.** With no `[audit.sink]` in any policy file, the
   dispatcher is never constructed: no file handle, no socket, no goroutine.
   A user must write the config to enable forwarding; `policy validate`
   rejects malformed sink config with line numbers before it can run.
3. **Bounded and fire-and-forget.** Delivery uses a bounded queue (default
   256); an overflowing or failing sink drops records and the drop/failure
   counts are written back to the *local* audit log as `sink_error` /
   `sink_dropped` events. One delivery attempt per record — no retry, no
   backoff, no unbounded work. Self-audit events are never themselves
   forwarded (no loop).
4. **The process may wait, briefly, after the decision.** A one-shot hook
   process gives the dispatcher a bounded drain window (default 500 ms,
   `[audit.sink] timeout_ms`) *after* the decision is delivered. This can
   extend the process wall time, never the decision latency (§8 budgets are
   verdict latencies and stay untouched — AC0.7.5 asserts it).
5. **Sink failure cannot change a verdict, ever** — same contract as the
   existing audit-write failure path (`TestAC_0_6_2_audit_write_failure_
   keeps_decision`), now extended to delivery (AC0.7.5).

## Consequences

- PRD §3.3's wording is clarified, not weakened: the decision path stays
  zero-network; forwarding is egress after delivery, opt-in, off by default.
  ROADMAP.md's not-scheduled line distinguishes remote *decisioning* (still
  banned) from post-decision forwarding (0.7.0).
- Failure modes for the sink (unreachable target, queue overflow, malformed
  config) are audited rows in `docs/failure-modes.md`, contract-checked like
  every other failure path.
- Malformed `[audit.sink]` config is an invalid policy → the hook fails
  closed, exactly like any other invalid policy (AC0.3.7's rule).

## Guards

`TestAC_0_7_5_sink_off_by_default` (no side effects unconfigured),
`TestAC_0_7_5_sink_failure_never_affects_verdict` (dead target: decision,
exit code and latency unaffected; failure audited),
`TestAC_0_7_6_sink_config_validate` (malformed config rejected with line
numbers).
