# Install glove for OpenCode

Time: ~2 minutes. You need the `glove` binary on your `PATH`.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5.

## 1. Get the binary

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build && sudo cp bin/glove /usr/local/bin/
glove version
```

## 2. Install the shim

```bash
glove install opencode
```

This writes `~/.config/opencode/plugin/glove.ts` (diff first, backup of any
replaced file, idempotent) — a generated plugin shim registering
`tool.execute.before` that spawns `glove hook opencode` per tool call and
reads the verdict JSON from stdout. Kilo shares this exact lineage (it is a
fork), so the shim machinery is identical and only the import/export shape
differs.

Decision behavior is the same as [Kilo](kilo.md): allow runs, review throws
(`GLOVE_REVIEW`), block throws with the rule reasons, and any glove failure
blocks — fail-closed.

## 3. Verify

```bash
glove doctor
glove hook opencode <<< '{"tool":"bash","args":{"command":"rm -rf /"},"session_id":"s","call_id":"c"}'
# → {"decision":"block",...}, exit 2
```

## OpenCode V2 caveat

The shim targets the stable **V1 plugin API** (`@opencode-ai/plugin` 1.x).
OpenCode's V2 beta replaces the plugin API — per its own migration doc,
"V1 plugin implementations do not run in V2". If you run a V2 beta build,
the glove shim will not load; when V2 ships stable, `glove install
opencode` will follow (the change is confined to `internal/adapter/`).
This page will say so; check `glove list` and this file if OpenCode's
plugins stop loading.

## Uninstall

```bash
glove uninstall opencode
```

## Troubleshooting

- **Shim not loading** — `glove doctor` verifies the file is intact
  byte-for-byte; a hand-edited shim reports "present but modified" until
  you re-run install. Restart OpenCode after installing.
- **Windows** — `%USERPROFILE%\.config\opencode\plugin\glove.ts`; the shim
  spawns `glove.exe`.
