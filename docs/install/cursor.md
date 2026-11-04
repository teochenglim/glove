# Install glove for Cursor

Time: ~2 minutes. You need the `glove` binary on your `PATH`.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5.

## 1. Get the binary

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build && sudo cp bin/glove /usr/local/bin/
glove version
```

## 2. Install the hook

Cursor has shipped agent hooks since 1.7 (2025-09). Glove installs one
`preToolUse` entry at user level, `~/.cursor/hooks.json` (Cursor reloads
hooks on save; a project-level `.cursor/hooks.json` would take precedence
over yours — check it if behavior differs inside a repo).

```bash
glove install cursor
```

The entry — note `failClosed: true`, load-bearing:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "command": "glove hook cursor",
        "timeout": 30,
        "failClosed": true,
        "matcher": ""
      }
    ]
  }
}
```

- `matcher: ""` — every tool call.
- `failClosed: true` — Cursor is **fail-open by default** ("Cursor logs the
  failure and allows the action through"). Without this flag, a crashed
  glove would open the gate. Glove sets it on install and `glove doctor`
  flags the entry as UNHEALTHY if it is ever disabled.

## 3. Verify

```bash
glove doctor
glove hook cursor <<< '{"hook_event_name":"preToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"},"conversation_id":"c1","cwd":"/repo"}'
# → {"permission":"deny","user_message":"rm_root: ..."}, exit 2
```

## Review renders as deny (documented deviation)

Cursor accepts `allow | deny | ask`, but per its docs `ask` is **not
enforced on `preToolUse` today** — an unenforced ask would let a glove
review sail through, which is the one thing the guard may never do. Glove
therefore renders review as `deny` with a `glove review — blocked pending
your confirmation` message: it tightens, never loosens. Approve such calls
manually (run `glove check "<cmd>"` to see the reasoning first).

## Uninstall

```bash
glove uninstall cursor
```

## Troubleshooting

- **Nothing blocks** — `glove doctor`; then make sure the user-level file
  isn't overridden by a project `.cursor/hooks.json`.
- **Cursor runs a cloud agent** — cloud agents ignore user-level
  `~/.cursor/hooks.json` (Cursor docs); enforcement there is Cursor-side.
- **`AskQuestion` skips preToolUse** — a known Cursor gap (their forum
  #161836); not a glove bug.
