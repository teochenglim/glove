# Install glove for Gemini CLI

Time: ~2 minutes. You need the `glove` binary on your `PATH`.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5.

## 1. Get the binary

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build && sudo cp bin/glove /usr/local/bin/
glove version
```

## 2. Install the hook

```bash
glove install gemini
```

Gemini CLI hooks are GA (`hooksConfig.enabled` defaults true). Glove
installs one `BeforeTool` group into `~/.gemini/settings.json` (diff first,
backup, idempotent — your other settings keys are preserved):

```json
{
  "hooks": {
    "BeforeTool": [
      {
        "hooks": [
          { "name": "glove", "type": "command", "command": "glove hook gemini", "timeout": 10000 }
        ]
      }
    ]
  }
}
```

No `matcher` — the hook fires on every tool call. The timeout is in
milliseconds (Gemini's unit); 10000 covers glove's own Layer-2 budget
(`[laya] hook_timeout_ms` defaults to 5000) with slack.

## 3. Verify

```bash
glove doctor
glove hook gemini <<< '{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"rm -rf /"},"session_id":"s1","cwd":"/repo"}'
# → {"decision":"deny","reason":"rm_root: ..."}, exit 2
```

In a live session: `rm -rf /` is denied before execution (Gemini shows the
reason to the agent as a tool error); `ls` runs.

## Decision mapping + fail-open note

- `allow` renders as **empty stdout** (exit 0) — Gemini's docs default to
  Allow on non-JSON stdout, so glove keeps stdout JSON-only and puts all
  logging on stderr.
- `block` prints `{"decision":"deny","reason":...}` **and exits 2**
  (Gemini's "system block"; the stderr reason is what the agent sees).
- `review` renders as deny with a `glove review — blocked pending your
  confirmation` message. Gemini has no ask verb on `BeforeTool`; an
  unenforceable ask would loosen the guard, so review tightens to deny.
- If Gemini's hooks are disabled (`hooksConfig.enabled: false` in any
  settings layer), nothing fires — `glove doctor` can't see that; check the
  agent's own settings if blocks stop happening.

## Uninstall

```bash
glove uninstall gemini
```

## Troubleshooting

- **Hook never fires** — `glove doctor` for the entry; then confirm hooks
  are enabled (`gemini` settings: `hooksConfig.enabled`) and that no
  project `.gemini/settings.json` layer overrides it. Project hooks are
  fingerprinted by Gemini — after glove edits, Gemini may ask to re-trust.
- **`command not found: glove`** — the hook resolves `glove` from `PATH`.
