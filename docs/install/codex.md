# Install glove for Codex CLI

Time: ~2 minutes. You need the `glove` binary on your `PATH`.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5.

## 1. Get the binary

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build && sudo cp bin/glove /usr/local/bin/
glove version
```

## 2. Install the hook

Codex has a Claude-Code-compatible hooks system (verified against
`codex-rs/hooks` schemas, 2026-11): `PreToolUse` hooks receive the tool
call as JSON on stdin and decide via stdout `permissionDecision` or exit
code 2.

```bash
glove install codex
```

This **shows a diff**, **backs up**, and writes one entry to
`~/.codex/hooks.json` (Codex merges this with any `[hooks]` tables in
`config.toml`):

```json
{
  "hooks": {
    "PreToolUse": [
      { "hooks": [ { "type": "command", "command": "glove hook codex", "timeout": 30 } ] }
    ]
  }
}
```

Installing again is a no-op. A broken `hooks.json` fails the install loudly
with nothing written.

## 3. Trust the hook

Codex hash-pins hooks and asks you to review them (the `/hooks` surface).
After installing, confirm the glove entry there — that is Codex's own trust
model, not glove's.

## 4. Verify

```bash
glove doctor
glove hook codex <<< '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"},"session_id":"s1","cwd":"/repo"}'
# → {"hookSpecificOutput":{"permissionDecision":"deny",...}}, exit 2
```

In a live session: `rm -rf /` is denied before execution, `ls` runs, gray
areas ask.

## Honest limits (Codex's trust model, not glove's)

- **Hosted tools bypass hooks entirely** (Codex docs say so) — glove cannot
  see them.
- **A hook error/timeout is fail-open on Codex's side**: if glove hangs
  past the 30s timeout, Codex proceeds. glove itself is bounded well below
  that, so this only trips on a truly stuck process.
- Treat Codex + glove as layered guardrails, per Codex's own docs.

## Uninstall

```bash
glove uninstall codex
```

## Troubleshooting

- **Hook never fires** — `glove doctor` must show the entry installed; then
  check `/hooks` in Codex (an untrusted hook is skipped until trusted).
- **`command not found: glove`** — the hook resolves `glove` from `PATH`.
