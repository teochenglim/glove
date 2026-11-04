# Install glove for Claude Code

Time: ~3 minutes. You need the `glove` binary on your `PATH`.

## 1. Get the binary

From a release tag (once `v0.1.0` is published, artifacts appear on the GitHub release): download the tarball for your platform, or build from source:

```bash
git clone https://github.com/teochenglim/glove && cd glove
make build          # → ./bin/glove
sudo cp bin/glove /usr/local/bin/   # or anywhere on your PATH
glove version       # → 0.1.0
```

## 2. Install the hook

```bash
glove install claude-code
```

This does, in order:

1. **Shows a diff** of the change it is about to make to `~/.claude/settings.json` (never writes blind — PRD §10.4). Preview without writing: `glove install claude-code --dry-run`.
2. **Backs up** your current settings to `~/.claude/settings.json.glove.bak`.
3. **Writes one hook entry** — no `matcher`, so it fires on every tool call:

```json
{
  "hooks": {
    "PreToolUse": [
      { "hooks": [ { "type": "command", "command": "glove hook claude-code" } ] }
    ]
  }
}
```

4. **Verifies itself** with two test calls (`ls -la` → allow, `rm -rf /` → block) and prints the result.

Installing again is a no-op ("already installed — no changes"). A broken `settings.json` makes install fail loudly and touch nothing.

## 3. Verify

```bash
glove doctor                      # rules loaded, hook entry present
glove check "rm -rf /"            # decision: block
glove check "cat ~/.ssh/id_rsa"   # decision: block (protected path)
glove check "cat ~/.ssh/config"   # decision: allow — config is not a credential
glove check "sudo apt install -y curl"   # decision: review
```

Then in a live Claude Code session: `rm -rf /` is denied before execution, `ls -la` runs normally, and anything force-confirm-class (`sudo`, `git push --force`, piping a download into `sh`) asks you first.

## What the hook does

On every `PreToolUse` event Claude Code pipes JSON to `glove hook claude-code` on stdin. Glove evaluates it against Layer 0 deterministic rules and prints a decision envelope:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"rm_root: recursive delete on /"}}
```

Exit codes: `0` allow/ask, `2` deny (belt and braces — even if stdout is lost, exit 2 blocks). If glove itself hits an internal error it **denies** — it never silently opens the gate.

## Uninstall

Restoration is two steps:

```bash
cp ~/.claude/settings.json.glove.bak ~/.claude/settings.json
```

(or remove the glove entry from `hooks.PreToolUse` by hand). A dedicated `glove uninstall claude-code` ships with a later release.

## Troubleshooting

- **`command not found: glove` inside hooks** — the hook resolves `glove` through `PATH`; keep it on a standard PATH (e.g. `/usr/local/bin`), not a project-local directory.
- **Nothing blocks** — run `glove doctor`; if it says "hook entry NOT installed", re-run `glove install claude-code` and restart Claude Code (hooks are read at session start).
- **Latency** — `glove doctor --bench hook` measures true cold start; the budget is 15ms median (PRD §8).
- **`--force-with-lease` is allowed on purpose** — bare `git push --force` reviews; the lease variant does not match the rule (RE2 has no lookahead; see `RELEASE/v0.1.0.md` notes).
