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

// For returns the adapter for a stable agent name; ok is false for unknown
// agents. New adapters register here as their releases land (kilo v0.5.0,
// codex/opencode v0.6.0, cursor/gemini/zed v0.8.0).
func For(name string) (Adapter, bool) {
	if name == ClaudeCodeName {
		return NewClaude(), true
	}
	return nil, false
}

// Names lists registered agent identifiers.
func Names() []string {
	return []string{ClaudeCodeName}
}
