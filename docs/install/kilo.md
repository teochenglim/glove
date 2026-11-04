# Install glove for Kilo (CLI)

Time: ~2 minutes. You need the `glove` binary on your `PATH`.

> **Screenshot**: pending the live-session pass — see `RELEASE.md` §5 for
> what has been verified locally vs. in a live session.

## 1. Get the binary

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build          # → ./bin/glove
sudo cp bin/glove /usr/local/bin/   # or anywhere on your PATH
glove version
```

## 2. Install the shim

Kilo is a fork of OpenCode and runs the same plugin system: TypeScript
files in `~/.config/kilo/plugin/` are loaded in-process (Bun) at startup.

```bash
glove install kilo
```

This:

1. **Shows the full shim content** as a diff before writing (`--dry-run` to
   preview without writing).
2. **Writes `~/.config/kilo/plugin/glove.ts`** — a generated shim that
   registers `tool.execute.before`. It spawns `glove hook kilo` with the
   tool call as JSON on stdin and reads the verdict from stdout. Backs up
   any prior file it replaces to `glove.ts.glove.bak`.
3. **Verifies itself** with two test calls (`ls -la` → allow, `rm -rf /` →
   block) and prints the result.

Installing again is a no-op ("already installed — no changes"). A foreign
file at the plugin path makes install refuse loudly — glove never
overwrites a file it did not generate.

## 3. What happens on a decision

| Verdict | Shim behavior |
|---|---|
| `allow` | returns without touching `output.args` — the call runs |
| `review` | throws with code `GLOVE_REVIEW` — the call never runs; the message asks for confirmation |
| `block` | throws — the call never runs; the rule reasons reach the model as the tool error text |
| glove missing / crash / exit 2 / unreadable output | **block** — the shim is fail-closed, never open |

## 4. Verify

```bash
glove doctor                      # kilo row: hook entry installed ✓
glove hook kilo <<< '{"tool":"bash","args":{"command":"rm -rf /"},"session_id":"s","call_id":"c"}'
# → {"decision":"block",...} and exit code 2
```

Then in a live Kilo session: `rm -rf /` errors before execution, `ls` runs
normally, `sudo …` asks first.

## Uninstall

```bash
glove uninstall kilo    # removes the shim (backs it up beside itself)
```

Then restart Kilo (plugins load at startup).

## Troubleshooting

- **Nothing blocks in Kilo** — run `glove doctor`; the shim must be at
  `~/.config/kilo/plugin/glove.ts` and Kilo restarted. `kilo debug paths`
  prints the resolved plugin dirs.
- **`command not found: glove` inside the shim** — the shim spawns `glove`
  from `PATH`; keep it on a standard PATH.
- **CI/sandboxed runs skip the shim entirely** — Kilo's `KILO_PURE=1`
  disables external plugins. Don't rely on glove in `KILO_PURE` sessions.
- **Windows** — the shim spawns `glove.exe` on Windows; the plugin path is
  `%USERPROFILE%\.config\kilo\plugin\glove.ts` (OpenCode's XDG logic).
