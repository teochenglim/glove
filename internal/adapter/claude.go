package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// claudeSettings is the subset of settings.json Glove reads and writes.
// Everything else is preserved untouched: the file is decoded into a
// generic map with json.Number (no float mangling) and re-marshalled
// indented — Claude Code settings are machine-managed JSON, and the diff
// shown before writing is the review surface (PRD §10.4).
func (a *Claude) Install(ctx context.Context, opts InstallOptions) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	path := a.settingsPath
	if opts.ConfigPath != "" {
		path = opts.ConfigPath
	}

	var original []byte
	if raw, err := os.ReadFile(path); err == nil {
		original = raw
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	cfg := map[string]any{}
	if len(original) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(original)))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			// AC0.1.4: fail loudly, touch nothing, no backup — the user must
			// not discover their settings were rewritten by glove.
			return fmt.Errorf("%s is not valid JSON: %w — nothing written, original untouched", path, err)
		}
	}

	if hasGloveHook(cfg, hookEntryFmt) {
		fmt.Fprintf(w, "%s: glove hook already installed — no changes\n", path)
		return nil
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		cfg["hooks"] = hooks
	}
	pre, _ := hooks["PreToolUse"].([]any)
	// No matcher field: the entry applies to every tool call (PRD §3.1 —
	// glove intercepts every tool invocation, not just Bash).
	group := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": hookEntryFmt}},
	}
	hooks["PreToolUse"] = append(pre, group)

	newJSON, err := marshalSettings(cfg)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	oldLines := splitLines(string(original))
	newLines := splitLines(string(newJSON))
	fmt.Fprintf(w, "glove install claude-code → %s\n", path)
	writeDiff(w, oldLines, newLines)

	if opts.DryRun {
		fmt.Fprintln(w, "dry run — nothing written")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	// Backup the immediately-prior state before the first modification.
	if err := os.WriteFile(path+".glove.bak", original, 0o600); err != nil {
		return fmt.Errorf("write backup %s.glove.bak: %w", path, err)
	}
	if err := os.WriteFile(path, newJSON, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(w, "installed (backup: %s.glove.bak)\n", path)
	return nil
}

// Uninstall removes every glove hook group from the settings file.
// Idempotent: nothing installed → no-op.
func (a *Claude) Uninstall(ctx context.Context) error {
	path := a.settingsPath
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	cfg := map[string]any{}
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w — nothing written", path, err)
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	pre, _ := hooks["PreToolUse"].([]any)
	kept := make([]any, 0, len(pre))
	for _, g := range pre {
		if groupHasGlove(g, hookEntryFmt) {
			continue
		}
		kept = append(kept, g)
	}
	hooks["PreToolUse"] = kept
	newJSON, err := marshalSettings(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".glove.bak", raw, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return os.WriteFile(path, newJSON, 0o600)
}

// HookInstalled reports whether a glove hook entry is present in the
// settings file (glove doctor status output).
func (a *Claude) HookInstalled() bool {
	raw, err := os.ReadFile(a.settingsPath)
	if err != nil {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	cfg := map[string]any{}
	if err := dec.Decode(&cfg); err != nil {
		return false
	}
	return hasGloveHook(cfg, hookEntryFmt)
}

// hookOut is the Claude Code PreToolUse decision envelope (PRD §11).
type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             any    `json:"updatedInput,omitempty"`
}

type hookOut struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

// claudeDecision maps the frozen contract onto Claude Code vocabulary (§3.1):
// allow → allow, review → ask, block → deny.
func claudeDecision(d contract.Decision) (string, error) {
	switch d {
	case contract.DecisionAllow:
		return "allow", nil
	case contract.DecisionReview:
		return "ask", nil
	case contract.DecisionBlock:
		return "deny", nil
	default:
		return "", fmt.Errorf("unknown decision %q", d)
	}
}

func (a *Claude) RenderVerdict(v contract.Verdict) ([]byte, error) {
	dec, err := claudeDecision(v.Decision)
	if err != nil {
		return nil, err
	}
	out := hookOut{HookSpecificOutput: hookSpecificOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       dec,
		PermissionDecisionReason: strings.Join(v.Reasons, "; "),
	}}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (a *Claude) ParseHookInput(raw []byte) (HookInput, error) {
	var wire struct {
		Event     string          `json:"hook_event_name"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return HookInput{}, fmt.Errorf("parse claude hook input: %w", err)
	}
	return HookInput{
		Agent:     ClaudeCodeName,
		Event:     wire.Event,
		ToolName:  wire.ToolName,
		ToolInput: wire.ToolInput,
		SessionID: wire.SessionID,
		CWD:       wire.CWD,
	}, nil
}

// --- settings helpers ---

func marshalSettings(cfg map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func hasGloveHook(cfg map[string]any, entry string) bool {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	pre, _ := hooks["PreToolUse"].([]any)
	for _, g := range pre {
		if groupHasGlove(g, entry) {
			return true
		}
	}
	return false
}

func groupHasGlove(group any, entry string) bool {
	g, ok := group.(map[string]any)
	if !ok {
		return false
	}
	hs, _ := g["hooks"].([]any)
	for _, h := range hs {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); cmd == entry {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// writeDiff prints a compact before/after diff: removals first, then
// additions, each in document order. Settings diffs are localized (one hook
// group), so a set-based line diff is enough to review what changes.
func writeDiff(w io.Writer, oldLines, newLines []string) {
	oldSet := map[string]int{}
	for _, l := range oldLines {
		oldSet[l]++
	}
	newSet := map[string]int{}
	for _, l := range newLines {
		newSet[l]++
	}
	for _, l := range oldLines {
		if newSet[l] > 0 {
			newSet[l]--
			continue
		}
		fmt.Fprintf(w, "  - %s\n", l)
	}
	for _, l := range newLines {
		if oldSet[l] > 0 {
			oldSet[l]--
			continue
		}
		fmt.Fprintf(w, "  + %s\n", l)
	}
}
