// Package adapter translates between agent-specific hook protocols and
// Glove's internal types (PRD §5.1, §5.2). Each adapter owns three things:
// where its agent's config lives, how its hook entry is written (idempotent,
// with a diff and a backup), and how a contract.Verdict is rendered to the
// agent's stdout.
package adapter

import (
	"context"
	"encoding/json"
	"io"

	"github.com/teochenglim/glove/pkg/contract"
)

// Adapter is the contract every Agent integration implements.
// It is responsible for translating Glove's internal Verdict into
// the Agent-specific hook output, and for knowing where its config lives.
type Adapter interface {
	// Name returns the stable identifier used in CLI ("claude-code", "kilo").
	Name() string

	// Detect reports whether the Agent is installed on this machine.
	Detect() (installed bool, configPath string, err error)

	// Install writes the Glove hook into the Agent's config.
	// Must be idempotent. Must show a diff before writing.
	Install(ctx context.Context, opts InstallOptions) error

	// Uninstall removes the Glove hook. Must be idempotent.
	Uninstall(ctx context.Context) error

	// HookEntry is the CLI command the Agent will invoke on each hook event.
	// e.g. "glove hook claude-code"
	HookEntry() string

	// RenderVerdict translates a Verdict into the bytes to write to stdout.
	RenderVerdict(v contract.Verdict) ([]byte, error)

	// ParseHookInput reads the Agent's stdin JSON into the internal HookInput.
	ParseHookInput(raw []byte) (HookInput, error)
}

// HookInput is the agent-agnostic tool-call description Glove evaluates (§5.2).
type HookInput struct {
	Agent     string          `json:"agent"`
	Event     string          `json:"event"`      // "PreToolUse", "PostToolUse"
	ToolName  string          `json:"tool_name"`  // "Bash", "Edit", "mcp__foo__bar"
	ToolInput json.RawMessage `json:"tool_input"` // Agent-specific shape
	SessionID string          `json:"session_id,omitempty"`
	CWD       string          `json:"cwd,omitempty"`
}

// InstallOptions carries install-time behaviour. Writer receives the config
// diff and progress output — an install must always show a diff (PRD §10.4);
// ConfigPath overrides the adapter's default settings file (tests first,
// project-level installs later); DryRun shows the diff and writes nothing.
type InstallOptions struct {
	DryRun     bool
	ConfigPath string
	Writer     io.Writer
}

// Enforcement modes reported in AgentStatus.Mode (PRD §7 0.5.0 — `glove
// list` marks zed as audit).
const (
	ModeHook  = "hook"  // pre-tool enforcement: allow / review / block before execution
	ModeAudit = "audit" // observation only — the agent exposes no pre-tool hook
)

// AgentStatus is what `glove list` and `glove doctor` report per agent
// (PRD §7 0.5.0). Problem is "" for a healthy chain; anything else is a
// human-readable defect (corrupted config, missing hook entry) that doctor
// surfaces as unhealthy.
type AgentStatus struct {
	Name          string
	Mode          string // ModeHook or ModeAudit
	Detected      bool   // the agent exists on this machine
	ConfigPath    string // where the agent's glove-facing config lives ("" for audit-only agents without one)
	HookInstalled bool   // glove's hook entry / shim is present
	Entry         string // the hook entry command (HookEntry())
	Problem       string // "" = healthy
}

// StatusReporter is optional: adapters able to introspect their own config
// implement it. `glove list` and `glove doctor` walk every registered
// adapter through it — health checking never required changes to the frozen
// Adapter interface itself (PRD-0.5.0-1).
type StatusReporter interface {
	Status() AgentStatus
}

// StatusOf returns the AgentStatus for any adapter that reports one; the
// zero-value fallback keeps list/doctor total over every registered adapter.
func StatusOf(a Adapter) AgentStatus {
	if sr, ok := a.(StatusReporter); ok {
		return sr.Status()
	}
	st := AgentStatus{Name: a.Name(), Mode: ModeHook, Entry: a.HookEntry()}
	if installed, cfgPath, err := a.Detect(); err == nil {
		st.Detected, st.ConfigPath = installed, cfgPath
	} else {
		st.Problem = err.Error()
	}
	return st
}

// For returns the adapter for a stable agent name; ok is false for unknown
// agents. Adapters register here as they land (PRD §7 0.5.0: kilo first —
// structurally different — then codex/opencode, then cursor/gemini/zed).
func For(name string) (Adapter, bool) {
	switch name {
	case ClaudeCodeName:
		return NewClaude(), true
	case KiloName:
		return NewKilo(), true
	case CodexName:
		return NewCodex(), true
	case OpenCodeName:
		return NewOpenCode(), true
	case CursorName:
		return NewCursor(), true
	case GeminiName:
		return NewGemini(), true
	case GooseName:
		return NewGoose(), true
	case ZedName:
		return NewZed(), true
	}
	return nil, false
}

// Names lists registered agent identifiers.
func Names() []string {
	return []string{
		ClaudeCodeName,
		KiloName,
		CodexName,
		OpenCodeName,
		CursorName,
		GeminiName,
		GooseName,
		ZedName,
	}
}
