# Glove policy cookbook

Working examples for `~/.config/glove/policy.toml` (user level) and `.glove.toml` (project level, found by walking up from your working directory). Precedence: **project > user > built-in**; scalar keys override by precedence, list keys (patterns, protected paths) **concatenate** across layers.

Validate any file before relying on it — syntax errors report the line:

```bash
glove policy validate ~/.config/glove/policy.toml
```

Every example is testable without touching your agents:

```bash
glove check --policy .glove.toml "the command you want to test" --explain
```

Patterns are Go regexp (RE2 — no lookahead). Protected paths are globs: `**` spans directories, `*` stays within one. In TOML, prefer single-quoted literal strings for regexes (`['\bsudo\b']`) — double-quoted TOML strings treat `\` as an escape.

## 1. Protect an organisation secrets directory

```toml
# ~/.config/glove/policy.toml
[paths]
protected = ["/srv/org-secrets/**"]
```

Any command referencing the directory (read **or** write) blocks. Verify: `glove check "cat /srv/org-secrets/api.key"` → block.

## 2. Force-confirm every deploy

```toml
[rules]
force_confirm = ['\bnpm\s+run\s+deploy\b', '\bmake\s+release\b']
```

Deploys still possible, but you confirm each one. Verify: `glove check "npm run deploy"` → review.

## 3. Paranoid mode: block all download-piped-to-shell, no review step

The builtin set already blocks `curl … | sh` outright (v0.2.0 composition rules). Add the variants the composition rules don't claim:

```toml
[rules]
hard_block = ['\|-*\s*(ba|z|da|k|)sh\b']
```

## 4. High-security machine: fail closed when the model is unavailable

From v0.4.0, `serve` may be down (or you may run `--fast`). Decide what happens to gray-area calls then:

```toml
[general]
on_model_unavailable = "block"   # default is "review"
```

Until v0.4.0 this is recorded but inert — Layer 2 does not exist yet.

## 5. CI runners and containers: fast mode

Skip the semantic layer everywhere and permanently (deterministic layers only):

```toml
[general]
mode = "fast"      # standard (default) | fast | offline
```

Fast-mode hooks stay within the 20ms P95 budget and set `model_available: false` in every verdict.

## 6. Monorepo: stricter paths for the one repo that holds credentials

```toml
# <monorepo-root>/.glove.toml
[paths]
protected = [
  "**/infrastructure/terraform/**.tfvars",
  "**/k8s/**/secret*.yaml",
]
```

Only takes effect inside the repo — other projects keep the user-level policy. Verify from a subdirectory: `glove check "cat infra/terraform/prod.tfvars"` → block.

## 7. Never let anyone read your cloud credentials

```toml
[paths]
protected = [
  "~/.aws/**",
  "~/.config/gcloud/**",
  "~/.azure/**",
  "~/.kube/config",
]
```

## 8. Force-confirm any sudo that isn't apt

```toml
[rules]
force_confirm = ['\bsudo\b(?!. *apt(-get)? +install)']
```

Wait — RE2 has no lookahead. Do it with two layers instead: keep the builtin `\bsudo\b` review and accept that apt installs also review. There is no allow-list in Glove by design (the guard only ever tightens, PRD §4.2); the honest lever is changing review → block:

```toml
[rules]
hard_block = ['\bsudo\s+rm\b']   # sudo rm: never, not even with confirmation
```

## 9. Layer 2: point glove at your laya container (and tune the review band)

```toml
[laya]
endpoint = "http://127.0.0.1:8080"     # the laya-serve container (loopback only)
image = "ghcr.io/teochenglim/laya"     # what `glove serve` offers to start
# image_digest = "sha256:..."          # optional pin — mismatch refuses to start
hook_timeout_ms = 5000                  # how long the hook waits for a verdict
confidence_review_band = [0.2, 0.9]     # wider band = more reviews, fewer auto-calls
```

The container itself (english default, multilingual checkpoint shown):

```bash
docker run -d --name glove-laya -p 127.0.0.1:8080:8080 ghcr.io/teochenglim/laya:multilingual-cpu
```

Without the container, glove stays on the deterministic layers and undecidable calls follow `on_model_unavailable`. Band mapping: inside → review, outside → confident allow (the noul flags always decide blocks).

## 10. Point a project at its own pattern set without touching the user file

```toml
# <repo>/.glove.toml
[rules]
hard_block = ['\bterraform\s+destroy\b(?!. *-target)']
force_confirm = ['\bgitleaks\b']
```

Verify: `glove check --policy .glove.toml "terraform destroy"` → block.

## 11. Disable glove for an agent you trust less with friction

```toml
[agents.kilo]
enabled = false
```

`glove install kilo` refuses on a disabled agent; existing hooks keep working until uninstalled.

## 12. Test a policy against the classics before you commit it

```bash
glove check --policy /tmp/new-policy.toml "rm -rf /"        # must stay block
glove check --policy /tmp/new-policy.toml "ls -la"          # must stay allow
glove policy validate /tmp/new-policy.toml
```

## Troubleshooting

- **`invalid escape in string '\s'`** — you used double quotes around a regex. Switch to single quotes (TOML literal strings): `['\bsudo\b']`.
- **Policy errors at hook time, not check time** — by design (AC0.3.7): an invalid policy fails the hook closed (`deny`, exit 2). Fix it with `glove policy validate` before relying on it.
- **Who supplied my policy?** — `glove doctor` prints `policy mode=… (user=… project=…)`; `glove check` prints the same line under its verdict.
