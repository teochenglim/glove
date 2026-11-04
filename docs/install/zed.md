# Zed + glove — audit mode (honest limits)

Zed has **no pre-tool hook surface**. As of 2026-11 there is no extension
or config event in Zed that intercepts an agent tool call before execution
— the docs have no hooks pages, and the extension API (Rust/WASM) covers
languages, themes, and MCP context servers only. Glove therefore **cannot
block before execution in Zed**, and `glove install zed` deliberately
writes nothing to your Zed config.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5.

## What glove offers in Zed: audit + on-demand evaluation

```bash
glove install zed    # prints the audit-mode statement — no config change
glove hook zed <<< '{"tool_name":"terminal","tool_input":{"command":"cat ~/.aws/credentials"}}'
# → {"decision":"block",...} — evaluated and written to the audit log
```

Every `glove hook zed` call is evaluated by the same Layer 0 + 1 + (optional
Layer 2) engine and lands in the same immutable audit log (`glove audit
tail`). Use it to check anything Zed's agent proposed, after the fact or on
demand.

## The recommended layered posture

1. **Zed Tool Permissions — coarse, pre-execution.** Zed's
   `agent.tool_permissions` (settings.json) is declarative policy with real
   teeth: per-tool `always_deny` / `always_confirm` / `always_allow`
   pattern rules (Rust regex), e.g. confirm anything matching `sudo\s+/`
   in the terminal tool. Zed also sandboxes `terminal` and `fetch`
   natively (Seatbelt/Bubblewrap). Configure these in Zed for the blunt
   edges — they fire before execution, which glove cannot do here.
2. **Glove — fine-grained evaluation and the immutable trail.** Use
   `glove hook zed` on demand, and `glove audit tail` / `glove audit query`
   as the post-hoc record of what was checked and why. The audit log
   carries zero plaintext by default.

## What we are watching

- Zed persists agent threads — tool calls included — to SQLite under its
  data dir (`~/Library/Application Support/Zed/threads/threads.db` on
  macOS). That is the natural future observation point for post-hoc
  ingestion; it is an internal, unversioned schema, so glove ships no
  reader for it in this release.
- External agents *hosted in Zed* (Claude Code, Gemini CLI over ACP) keep
  their own hook surfaces — installing glove for those agents still
  protects them inside Zed.
- If Zed ships a real pre-tool hook, it becomes a normal adapter
  (`docs/adapter-authoring.md`) — one file, behind the conformance suite.

`glove list` shows zed as `mode: audit` — that is the designed posture, not
a defect; `glove doctor` reports it healthy.
