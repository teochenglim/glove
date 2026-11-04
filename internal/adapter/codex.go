package adapter

import (
	"context"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// CodexName is the stable adapter identifier used in the CLI.
const CodexName = "codex"

// hookEntryCodex is the command Codex invokes on every PreToolUse event.
const hookEntryCodex = "glove hook codex"

// codexHookTimeoutSecs bounds a hung glove inside Codex's hook runner
// (seconds; Codex's own default is 600). glove is bounded internally by
// [laya] hook_timeout_ms, so this only trips when the process is stuck.
// Codex is fail-open on hook timeout/error — documented residual risk in
// docs/install/codex.md; a stuck glove there means Codex proceeds, which is
// why glove's own hard exit-2 blocks (honoured by Codex) stay the primary
// enforcement path.
const codexHookTimeoutSecs = 30

// Codex adapts OpenAI Codex CLI: PreToolUse hooks in ~/.codex/hooks.json —
// Claude-Code-compatible wire protocol (verified against codex-rs/hooks
// schemas, 2026-11): stdin hook_event_name/tool_name/tool_input/session_id/
// cwd, stdout hookSpecificOutput.permissionDecision allow|deny|ask, exit 2
// blocks. Known limits, documented in docs/install/codex.md: hooks must be
// trusted in Codex (`/hooks`), hosted tools bypass hooks entirely, and a
// hook error/timeout is fail-open on Codex's side.
type Codex struct {
	hooksPath string
}

// NewCodex returns the adapter for the default hooks path.
func NewCodex() *Codex { return &Codex{hooksPath: paths.CodexHooksPath()} }

// NewCodexAt returns the adapter pinned to a specific hooks file (tests).
func NewCodexAt(path string) *Codex { return &Codex{hooksPath: path} }

func (a *Codex) Name() string { return CodexName }

func (a *Codex) HookEntry() string { return hookEntryCodex }

func (a *Codex) Detect() (bool, string, error) {
	return detectConfigDir(a.hooksPath, "codex")
}

// codexMutate appends one glove hook group under hooks.PreToolUse — same
// shape as Claude Code's settings, plus an explicit timeout.
func codexMutate(entry string, timeoutSecs int) jsonMutate {
	return func(cfg map[string]any) (present, changed bool, err error) {
		if hasGloveHookIn(cfg, "PreToolUse", entry) {
			return true, false, nil
		}
		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			cfg["hooks"] = hooks
		}
		pre, _ := hooks["PreToolUse"].([]any)
		group := map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": entry,
				"timeout": jsonNumber(timeoutSecs),
			}},
		}
		hooks["PreToolUse"] = append(pre, group)
		return false, true, nil
	}
}

func (a *Codex) Install(ctx context.Context, opts InstallOptions) error {
	return installJSONHook(a.hooksPath, CodexName, opts, codexMutate(hookEntryCodex, codexHookTimeoutSecs))
}

func (a *Codex) Uninstall(ctx context.Context) error {
	return removeJSONHook(a.hooksPath, "PreToolUse", hookEntryCodex)
}

func (a *Codex) Status() AgentStatus {
	return statusJSONHook(a, a.hooksPath, func(cfg map[string]any) bool {
		return hasGloveHookIn(cfg, "PreToolUse", hookEntryCodex)
	}, nil, nil)
}

func (a *Codex) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderPermissionDecision("PreToolUse", v)
}

func (a *Codex) ParseHookInput(raw []byte) (HookInput, error) {
	return parseClaudeStyleInput(CodexName, raw)
}
