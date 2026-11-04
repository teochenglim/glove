package adapter

import (
	"context"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// CursorName is the stable adapter identifier used in the CLI.
const CursorName = "cursor"

// hookEntryCursor is the command Cursor invokes on every preToolUse event.
const hookEntryCursor = "glove hook cursor"

// cursorHookTimeoutSecs bounds a hung glove inside Cursor's hook runner
// (seconds, as shown in the Cursor hooks docs examples). Same fail-open
// caveat as codex — documented in docs/install/cursor.md.
const cursorHookTimeoutSecs = 30

// Cursor adapts Cursor: hooks landed in Cursor 1.7 (2025-09); the config is
// a JSON file at ~/.cursor/hooks.json (user level; project-level
// .cursor/hooks.json also exists but glove installs the user-level file by
// default). One preToolUse entry, matcher "" = every tool call, and —
// load-bearing — failClosed: true. Cursor is fail-open by default ("Cursor
// logs the failure and allows the action through"), which would let a
// crashed glove open the gate; failClosed keeps the tighten-only invariant
// under glove faults.
//
// Deviation from §3.1, documented in docs/install/cursor.md: glove review
// renders as "deny" (with a review-prefixed message), not "ask" — Cursor
// does not enforce "ask" on preToolUse today, and an unenforced ask would
// let a review sail through. The guard only ever tightens.
type Cursor struct {
	hooksPath string
}

// NewCursor returns the adapter for the default user-level hooks path.
func NewCursor() *Cursor { return &Cursor{hooksPath: paths.CursorHooksPath()} }

// NewCursorAt returns the adapter pinned to a specific hooks file (tests).
func NewCursorAt(path string) *Cursor { return &Cursor{hooksPath: path} }

func (a *Cursor) Name() string { return CursorName }

func (a *Cursor) HookEntry() string { return hookEntryCursor }

func (a *Cursor) Detect() (bool, string, error) {
	return detectConfigDir(a.hooksPath, "cursor")
}

// cursorPresent detects glove's flat preToolUse entry and — because a
// failClosed:false glove entry weakens the invariant — reports a Problem
// through the status layer when the entry exists but lost failClosed.
func cursorPresent(cfg map[string]any, entry string) (present, degraded bool) {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return false, false
	}
	entries, _ := hooks["preToolUse"].([]any)
	for _, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := em["command"].(string); cmd == entry {
			fc, _ := em["failClosed"].(bool)
			return true, !fc
		}
	}
	return false, false
}

// cursorMutate inserts the glove entry: {"version":1,"hooks":{"preToolUse":
// [{type,command,timeout,failClosed,matcher}]}}. version:1 is required by
// the schema; existing user entries are preserved.
func cursorMutate(entry string, timeoutSecs int) jsonMutate {
	return func(cfg map[string]any) (present, changed bool, err error) {
		if present, _ := cursorPresent(cfg, entry); present {
			return true, false, nil
		}
		if _, ok := cfg["version"]; !ok {
			cfg["version"] = jsonNumber(1)
		}
		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			cfg["hooks"] = hooks
		}
		pre, _ := hooks["preToolUse"].([]any)
		hooks["preToolUse"] = append(pre, map[string]any{
			"type":       "command",
			"command":    entry,
			"timeout":    jsonNumber(timeoutSecs),
			"failClosed": true,
			"matcher":    "",
		})
		return false, true, nil
	}
}

func (a *Cursor) Install(ctx context.Context, opts InstallOptions) error {
	return installJSONHook(a.hooksPath, CursorName, opts, cursorMutate(hookEntryCursor, cursorHookTimeoutSecs))
}

func (a *Cursor) Uninstall(ctx context.Context) error {
	return removeJSONHook(a.hooksPath, "preToolUse", hookEntryCursor)
}

func (a *Cursor) Status() AgentStatus {
	return statusJSONHook(a, a.hooksPath, func(cfg map[string]any) bool {
		present, _ := cursorPresent(cfg, hookEntryCursor)
		return present
	}, func(st *AgentStatus, cfg map[string]any) {
		// A glove entry with failClosed disabled is a health problem, not a
		// green light — Cursor would allow tool calls through a crashed
		// glove, breaking the tighten-only invariant.
		if _, degraded := cursorPresent(cfg, hookEntryCursor); degraded {
			st.Problem = "glove hook entry has failClosed disabled — Cursor will allow tool calls if glove crashes; re-run `glove install cursor`"
			st.HookInstalled = false
		}
	}, nil)
}

func (a *Cursor) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderCursorVerdict(v)
}

func (a *Cursor) ParseHookInput(raw []byte) (HookInput, error) {
	return parseClaudeStyleInput(CursorName, raw)
}
