# SIEM ingestion guide (v0.7.0)

glove's audit trail is SIEM-ready out of the box: every record is part of a
tamper-evident hash chain, carries machine identity for fleet aggregation,
and tags the MITRE ATT&CK techniques of the rules that fired. This page is
the ingestion recipe per consumer class. The boundary is ADR 4: **forwarding
is post-decision, opt-in, and off by default** — a guard decision never
reads or waits on the network.

## What one record gives you

| Field (JSONL) | ECS | CEF | Meaning |
|---|---|---|---|
| `ts` | `@timestamp` | `start` | decision time (UTC) |
| `audit_id` | `event.id` | (msgid in syslog) | unique decision id |
| `decision` | `event.outcome` (+ `glove.decision`) | severity 2/5/8 | allow / review / block |
| `risk` | `glove.risk` | (+2 severity when critical) | low / medium / high / critical |
| `tool`, `agent` | `event.action`, `agent.name` | class id, `app` | what was attempted, by which agent |
| `input_hash` | `glove.input_hash` | `fileHash` | SHA-256 of the tool input — zero plaintext |
| `host` / `user` / `machine_id` | `agent.name` / `user.name` / `glove.machine_id` | `dhost` / `suser` / `cs3` | fleet join keys |
| `techniques` | `glove.techniques` | `cs2` | MITRE ATT&CK tags, e.g. `T1552.001` |
| `prev_hash` / `record_hash` | `glove.*` | — | hash-chain links (tamper evidence) |

## Choosing a transport

```toml
# syslog — the classic SIEM collector (rsyslog, syslog-ng, Splunk HEC
# via syslog, Elastic Agent's UDP input …). Off by default; add to enable.
[audit.sink]
type = "syslog"
target = "127.0.0.1:514"     # or a remote collector of yours
protocol = "udp"             # udp (default) | tcp | tcp+tls | unix
```

```toml
# webhook — HTTP collectors (Splunk HEC, Elastic ingest, OTEL bridges …).
[audit.sink]
type = "webhook"
target = "https://collector.internal:8443/glove"
timeout_ms = 500             # post-decision drain window
```

```toml
# file — for a filebeat/fluent-bit tail or an airgapped export workflow.
[audit.sink]
type = "file"
target = "/var/log/glove/forward.jsonl"
```

Delivery is one attempt per record with a bounded queue (default 256): a
down collector drops records rather than ever delaying or changing a
decision, and the drop/failure is written back to the **local** audit log as
`sink_error` / `sink_dropped` events — monitor for those events; they are
your completeness signal.

## Tamper evidence

```bash
glove audit verify
# ok: 4213 record(s) verified (17 legacy pre-0.7.0, unchained)
```

Every record hashes the previous one (`prev_hash` → `record_hash`), across
rotation boundaries. `audit verify` re-computes the chain and names the
first broken record (`edited`, `chain-gap`, `corrupt-line`). Run it on a
schedule (cron, CI job, or the SIEM's own script input) and alert on exit 1.
Limits, stated honestly: the chain detects any *edit*, any *middle
deletion*, and truncation; it cannot detect removal of the *tail* (an
append-only log has no forward anchor) — if you need that, forward records
off-box in near-real-time, which is exactly what the sink is for. The first
surviving record's `prev_hash` is the anchor: retention (10 MB × 5) drops
the oldest file, and its linkage cannot be validated retroactively.

## ATT&CK queries

Records inherit the technique tags of every rule that fired
(`docs/install/`'s vendor caveats do not apply here — tagging is engine-side
and adapter-independent):

```bash
glove audit query --technique T1552 --since 24h   # credential access
glove audit export --format ecs --since 7d > glove-week.ndjson
```

User patterns are tagged via `[rules.techniques]` in `policy.toml` (PRD
§5.5); untagged patterns contribute no technique rather than a wrong one.
The builtin mapping is deliberately technique-level (coarse) — the `reasons`
field stays the precise story.
