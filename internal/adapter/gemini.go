package adapter

import (
	"context"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// GeminiName is the stable adapter identifier used in the CLI.
const GeminiName = "gemini"

// hookEntryGemini is the command Gemini CLI invokes on every BeforeTool
// event.
const hookEntryGemini = "glove hook gemini"

// geminiHookTimeoutMs bounds a hung glove inside Gemini's hook runner
// (milliseconds in Gemini's schema, default 60000). Covers the laya round
// trip ([laya] hook_timeout_ms default 5000) with slack.
const geminiHookTimeoutMs = 10000

// Gemini adapts Google Gemini CLI: BeforeTool hooks configured in
// ~/.gemini/settings.json under "hooks" (hooks GA, hooksConfig.enabled
// defaults true; docs/hooks/reference.md, verified 2026-11). stdin carries
// tool_name/tool_input/session_id/cwd/hook_event_name; blocking is stdout
// {"decision":"deny","reason":...} or exit 2 (system block, stderr as
// reason). Gemini fails OPEN on non-JSON stdout ("The CLI will default to
// 'Allow'"), so the glove hook prints JSON on stdout only and keeps all
// logging on stderr. review renders as deny (Gemini has no ask verb on
// BeforeTool) — tighten-only; documented in docs/install/gemini.md.
type Gemini struct {
	settingsPath string
}

// NewGemini returns the adapter for the default user settings path.
func NewGemini() *Gemini { return &Gemini{settingsPath: paths.GeminiSettingsPath()} }

// NewGeminiAt returns the adapter pinned to a specific settings file (tests).
func NewGeminiAt(path string) *Gemini { return &Gemini{settingsPath: path} }

func (a *Gemini) Name() string { return GeminiName }

func (a *Gemini) HookEntry() string { return hookEntryGemini }

func (a *Gemini) Detect() (bool, string, error) {
	return detectConfigDir(a.settingsPath, "gemini")
}

// geminiMutate appends a glove group under hooks.BeforeTool. matcher is
// omitted (matches every tool). Existing user settings are preserved —
// settings.json holds unrelated (sometimes secret-bearing) keys.
func geminiMutate(entry string, timeoutMs int) jsonMutate {
	return func(cfg map[string]any) (present, changed bool, err error) {
		if hasGloveHookIn(cfg, "BeforeTool", entry) {
			return true, false, nil
		}
		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			cfg["hooks"] = hooks
		}
		before, _ := hooks["BeforeTool"].([]any)
		group := map[string]any{
			"hooks": []any{map[string]any{
				"name":    "glove",
				"type":    "command",
				"command": entry,
				"timeout": jsonNumber(timeoutMs),
			}},
		}
		hooks["BeforeTool"] = append(before, group)
		return false, true, nil
	}
}

func (a *Gemini) Install(ctx context.Context, opts InstallOptions) error {
	return installJSONHook(a.settingsPath, GeminiName, opts, geminiMutate(hookEntryGemini, geminiHookTimeoutMs))
}

func (a *Gemini) Uninstall(ctx context.Context) error {
	return removeJSONHook(a.settingsPath, "BeforeTool", hookEntryGemini)
}

func (a *Gemini) Status() AgentStatus {
	return statusJSONHook(a, a.settingsPath, func(cfg map[string]any) bool {
		return hasGloveHookIn(cfg, "BeforeTool", hookEntryGemini)
	}, nil, nil)
}

func (a *Gemini) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderGeminiVerdict(v)
}

func (a *Gemini) ParseHookInput(raw []byte) (HookInput, error) {
	return parseClaudeStyleInput(GeminiName, raw)
}
