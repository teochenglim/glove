package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// ClaudeCodeName is the stable adapter identifier used in the CLI.
const ClaudeCodeName = "claude-code"

// hookEntryFmt is the command Claude Code invokes on every PreToolUse event.
// Bare name on PATH (PRD §5.1); the install guide tells users to keep glove
// on PATH.
const hookEntryFmt = "glove hook claude-code"

// Claude adapts Claude Code: stdin JSON on PreToolUse (§11), stdout decision
// JSON, hooks stored in ~/.claude/settings.json.
type Claude struct {
	settingsPath string
}

// NewClaude returns the adapter for the default user-level settings path.
func NewClaude() *Claude {
	return &Claude{settingsPath: paths.ClaudeSettingsPath()}
}

// NewClaudeAt returns the adapter pinned to a specific settings file
// (tests; project-level installs from v0.7.0).
func NewClaudeAt(path string) *Claude {
	return &Claude{settingsPath: path}
}

func (a *Claude) Name() string { return ClaudeCodeName }

func (a *Claude) HookEntry() string { return hookEntryFmt }

func (a *Claude) Detect() (bool, string, error) {
	if _, err := os.Stat(a.settingsPath); err == nil {
		return true, a.settingsPath, nil
	}
	// The settings file may not exist yet while the CLI is installed.
	if p, err := exec.LookPath("claude"); err == nil && p != "" {
		return true, a.settingsPath, nil
	}
	return false, a.settingsPath, nil
}

// claudeMutate appends one glove hook group under hooks.PreToolUse. No
// matcher field: the entry applies to every tool call (PRD §3.1 — glove
// intercepts every tool invocation, not just Bash).
func claudeMutate(entry string) jsonMutate {
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
			"hooks": []any{map[string]any{"type": "command", "command": entry}},
		}
		hooks["PreToolUse"] = append(pre, group)
		return false, true, nil
	}
}

func (a *Claude) Install(ctx context.Context, opts InstallOptions) error {
	return installJSONHook(a.settingsPath, ClaudeCodeName, opts, claudeMutate(hookEntryFmt))
}

// Uninstall removes every glove hook group from the settings file.
// Idempotent: nothing installed → no-op.
func (a *Claude) Uninstall(ctx context.Context) error {
	return removeJSONHook(a.settingsPath, "PreToolUse", hookEntryFmt)
}

// HookInstalled reports whether a glove hook entry is present in the
// settings file. Kept for callers predating Status(); doctor/list read
// Status() now.
func (a *Claude) HookInstalled() bool {
	return a.Status().HookInstalled
}

// Status reports the claude-code chain health for `glove list` / `glove
// doctor` (PRD §7 0.5.0): a present-but-invalid settings.json is a Problem.
func (a *Claude) Status() AgentStatus {
	return statusJSONHook(a, a.settingsPath, func(cfg map[string]any) bool {
		return hasGloveHookIn(cfg, "PreToolUse", hookEntryFmt)
	}, nil, nil)
}

// hookOut/hookSpecificOutput keep their historical names as aliases of the
// shared claude-code/codex envelope (claude_test.go decode targets).
type hookSpecificOutput = permissionDecisionBody
type hookOut = permissionDecisionOut

func (a *Claude) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderPermissionDecision("PreToolUse", v)
}

func (a *Claude) ParseHookInput(raw []byte) (HookInput, error) {
	return parseClaudeStyleInput(ClaudeCodeName, raw)
}

// hasGloveHook keeps the historical claude-shaped helper (tests read it)
// over the shared event-parameterized form.
func hasGloveHook(cfg map[string]any, entry string) bool {
	return hasGloveHookIn(cfg, "PreToolUse", entry)
}
