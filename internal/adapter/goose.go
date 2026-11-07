package adapter

import (
	"bytes"
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

// GooseName is the stable adapter identifier used in the CLI.
const GooseName = "goose"

// hookEntryGoose is the command goose invokes on every PreToolUse event.
const hookEntryGoose = "glove hook goose"

// gooseHookTimeoutSecs bounds a hung glove inside goose's hook runner.
// Goose's own default is 30s; glove tightens it — its internal wait is
// bounded by [laya] hook_timeout_ms (default 5000ms), so 15s only trips
// when the process is genuinely stuck. Combined with on_failure: "block"
// below, a stuck glove BLOCKS the tool call — the fail-closed posture
// goose explicitly supports (verified 2026-11-07, aaif-goose hooks docs).
const gooseHookTimeoutSecs = 15

// Goose adapts the goose agent (aaif-goose, the Linux-Foundation-governed
// agent): a native BLOCKING PreToolUse hook registered through a plugin
// manifest at ~/.agents/plugins/glove/hooks/hooks.json — no shim, no MCP
// proxy. Wire (verified 2026-11-07, issue #8742 → PRs #9093/#9304):
// stdin JSON (event, session_id, tool_name, tool_input, working_dir,
// tool_call_id); decision on stdout as {"decision":"allow"} or
// {"decision":"block","reason":…}; exit 2 + stderr reason also blocks;
// ANYTHING else (stray stdout, malformed JSON, timeout, spawn error) is
// "no decision" and the tool proceeds — which is why glove prints
// JSON-only stdout and registers on_failure: "block".
//
// Known limits, documented in docs/install/goose.md: goose hook decisions
// are allow/block only — review renders as deny (tighten-only, ADR 1);
// the matcher is a regex, not a glob; invalid hooks.json values make goose
// skip the plugin's entire manifest with a warning (the grammar is
// test-pinned); SubagentStart/Stop are never emitted (subagent tool-call
// coverage is verified on implementation day and recorded in the release
// file).
type Goose struct {
	hooksPath    string
	gooseCfgPath string
}

// NewGoose returns the adapter for the default plugin path.
func NewGoose() *Goose {
	return &Goose{hooksPath: paths.GooseHooksPath(), gooseCfgPath: gooseSettingsPath()}
}

// NewGooseAt returns the adapter pinned to a specific hooks manifest
// (tests).
func NewGooseAt(path string) *Goose {
	return &Goose{hooksPath: path, gooseCfgPath: gooseSettingsPath()}
}

// gooseSettingsPath is goose's own settings file (disabledPlugins lives
// there). Empty when there is no home context.
func gooseSettingsPath() string {
	h := paths.HomeDir()
	if h == "" {
		return ""
	}
	return filepath.Join(h, ".config", "goose", "settings.json")
}

func (a *Goose) Name() string { return GooseName }

func (a *Goose) HookEntry() string { return hookEntryGoose }

// Detect reports goose's presence: the plugin manifest installed, goose's
// own config dir present, or the goose binary on PATH.
func (a *Goose) Detect() (bool, string, error) {
	if _, err := os.Stat(a.hooksPath); err == nil {
		return true, a.hooksPath, nil
	}
	if a.gooseCfgPath != "" {
		if _, err := os.Stat(a.gooseCfgPath); err == nil {
			return true, a.hooksPath, nil
		}
	}
	if p, err := exec.LookPath("goose"); err == nil && p != "" {
		return true, a.hooksPath, nil
	}
	return false, a.hooksPath, nil
}

// gooseHooksJSON renders the plugin manifest goose loads. Kept minimal and
// strictly inside goose's grammar (top-level hooks → event → rules with an
// optional regex matcher → command actions) — invalid values make goose
// skip the plugin's entire manifest with a warning, so every key here is
// one goose defines. The matcher is omitted deliberately: unset matches
// every tool.
func gooseHooksJSON(entry string, timeoutSecs int) []byte {
	manifest := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":       "command",
							"command":    entry,
							"timeout":    timeoutSecs,
							"on_failure": "block",
						},
					},
				},
			},
		},
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil // only strings/ints; unreachable
	}
	return append(b, '\n')
}

// isGloveHooksJSON reports whether raw is a manifest glove wrote (parseable
// and carrying glove's hook entry under PreToolUse) — the ownership marker
// for install/uninstall/status, since JSON admits no comment marker.
func isGloveHooksJSON(raw []byte, entry string) bool {
	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return false
	}
	for _, rule := range manifest.Hooks.PreToolUse {
		for _, h := range rule.Hooks {
			if h.Type == "command" && h.Command == entry {
				return true
			}
		}
	}
	return false
}

func (a *Goose) Install(ctx context.Context, opts InstallOptions) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	target := a.hooksPath
	if opts.ConfigPath != "" {
		target = opts.ConfigPath
	}
	content := gooseHooksJSON(hookEntryGoose, gooseHookTimeoutSecs)

	var original []byte
	if raw, err := os.ReadFile(target); err == nil {
		original = raw
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", target, err)
	}
	if len(original) > 0 && !isGloveHooksJSON(original, hookEntryGoose) {
		// AC0.5.1 broken-config guard, shim flavour: the manifest belongs
		// to someone else (or is corrupt) — glove must not replace it.
		return fmt.Errorf("%s exists and is not a glove shim (not a glove goose hooks manifest) — refusing to overwrite; move it aside or point install at another path", target)
	}
	if bytes.Equal(original, content) {
		fmt.Fprintf(w, "%s: glove hooks manifest already installed — no changes\n", target)
		return nil
	}

	fmt.Fprintf(w, "glove install %s → %s\n", GooseName, target)
	writeDiff(w, splitLines(string(original)), splitLines(string(content)))
	if opts.DryRun {
		fmt.Fprintln(w, "dry run — nothing written")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	if len(original) > 0 {
		if err := os.WriteFile(target+".glove.bak", original, 0o644); err != nil {
			return fmt.Errorf("write backup %s.glove.bak: %w", target, err)
		}
	}
	// 0644: the manifest carries no secrets and is read by goose's plugin
	// discovery.
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	if len(original) > 0 {
		fmt.Fprintf(w, "installed (backup: %s.glove.bak)\n", target)
	} else {
		fmt.Fprintf(w, "installed\n")
	}
	return nil
}

func (a *Goose) Uninstall(ctx context.Context) error {
	return a.uninstall(os.Stdout)
}

func (a *Goose) uninstall(w io.Writer) error {
	raw, err := os.ReadFile(a.hooksPath)
	if os.IsNotExist(err) {
		fmt.Fprintf(w, "%s: no glove hooks manifest present — nothing to do\n", a.hooksPath)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", a.hooksPath, err)
	}
	if !isGloveHooksJSON(raw, hookEntryGoose) {
		return fmt.Errorf("%s is not a glove shim — leaving it alone", a.hooksPath)
	}
	if err := os.WriteFile(a.hooksPath+".glove.bak", raw, 0o644); err != nil {
		return fmt.Errorf("write backup %s.glove.bak: %w", a.hooksPath, err)
	}
	if err := os.Remove(a.hooksPath); err != nil {
		return fmt.Errorf("remove %s: %w", a.hooksPath, err)
	}
	// Drop the now-empty plugin dirs glove created (never errors — the dirs
	// may hold someone else's files).
	_ = os.Remove(filepath.Dir(a.hooksPath))
	_ = os.Remove(filepath.Dir(filepath.Dir(a.hooksPath)))
	fmt.Fprintf(w, "removed %s (backup: %s)\n", a.hooksPath, a.hooksPath+".glove.bak")
	return nil
}

// Status implements StatusReporter: healthy when the manifest is present,
// intact, and still fail-closed; unhealthy when it was corrupted, replaced,
// lost its on_failure: block, or goose disabled the plugin.
func (a *Goose) Status() AgentStatus {
	st := AgentStatus{
		Name:       GooseName,
		Mode:       ModeHook,
		ConfigPath: a.hooksPath,
		Entry:      hookEntryGoose,
	}
	st.Detected, st.ConfigPath, _ = a.Detect()
	raw, err := os.ReadFile(a.hooksPath)
	if os.IsNotExist(err) {
		return st // healthy, just not installed
	}
	if err != nil {
		st.Problem = fmt.Sprintf("read hooks manifest: %v", err)
		return st
	}
	if !isGloveHooksJSON(raw, hookEntryGoose) {
		st.Problem = "hooks manifest exists but is not a glove hooks file — refusing to manage it"
		return st
	}
	// The fail-closed posture is the health signal: a manifest that lost
	// on_failure: "block" (hand-edited, downgraded) degrades to goose's
	// fail-open default without anyone noticing — surface it loudly.
	failClosed := false
	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command   string `json:"command"`
					Timeout   int    `json:"timeout"`
					OnFailure string `json:"on_failure"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	_ = json.Unmarshal(raw, &manifest)
	for _, rule := range manifest.Hooks.PreToolUse {
		for _, h := range rule.Hooks {
			if h.Command == hookEntryGoose && h.OnFailure == "block" {
				failClosed = true
			}
		}
	}
	if !failClosed {
		st.Problem = "PreToolUse rule lost on_failure: \"block\" — reinstall to restore the fail-closed posture"
		return st
	}
	st.HookInstalled = true
	if a.gooseCfgPath != "" {
		if raw, err := os.ReadFile(a.gooseCfgPath); err == nil {
			var settings struct {
				DisabledPlugins []string `json:"disabledPlugins"`
			}
			if json.Unmarshal(raw, &settings) == nil {
				for _, name := range settings.DisabledPlugins {
					if name == GooseName {
						st.Problem = "glove plugin is listed in disabledPlugins — goose will not run the hook"
						return st
					}
				}
			}
		}
	}
	return st
}

// goosePayload is goose's PreToolUse stdin (verified 2026-11-07): always
// event + session_id, conditionally the tool fields; built-in tools carry
// typed inputs (shell → command + timeout_secs, write → path + content,
// edit → path + before + after).
type goosePayload struct {
	Event      string          `json:"event"`
	SessionID  string          `json:"session_id"`
	ToolName   string          `json:"tool_name"`
	ToolInput  json.RawMessage `json:"tool_input"`
	WorkingDir string          `json:"working_dir"`
	ToolCallID string          `json:"tool_call_id"`
}

func (a *Goose) ParseHookInput(raw []byte) (HookInput, error) {
	var p goosePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return HookInput{}, fmt.Errorf("invalid goose hook input: %w", err)
	}
	if p.ToolName == "" {
		return HookInput{}, fmt.Errorf("invalid goose hook input: missing tool_name")
	}
	return HookInput{
		Agent:     GooseName,
		Event:     "PreToolUse",
		ToolName:  p.ToolName,
		ToolInput: p.ToolInput,
		SessionID: p.SessionID,
		CWD:       p.WorkingDir,
	}, nil
}

// RenderVerdict renders into goose's hook decision vocabulary. Allow is an
// explicit {"decision":"allow"} (goose also accepts empty stdout, but the
// explicit form is unambiguous); review and block both render
// {"decision":"block","reason":…} — goose hooks are allow/block only, so
// review tightens to deny (ADR 1) and the reason says so. hook.go also
// exits 2 with the reason on stderr for blocks, which goose honours
// independently of the JSON.
func (a *Goose) RenderVerdict(v contract.Verdict) ([]byte, error) {
	switch v.Decision {
	case contract.DecisionAllow:
		return []byte(`{"decision":"allow"}`), nil
	case contract.DecisionReview:
		reason := strings.Join(v.Reasons, "; ")
		if reason == "" {
			reason = "undecidable call"
		}
		return gooseBlockJSON(reason + " — review rendered as deny (goose hooks are allow/block only)"), nil
	default:
		reason := strings.Join(v.Reasons, "; ")
		if reason == "" {
			reason = "blocked by glove policy"
		}
		return gooseBlockJSON(reason), nil
	}
}

func gooseBlockJSON(reason string) []byte {
	b, err := json.Marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{Decision: "block", Reason: reason})
	if err != nil {
		return []byte(`{"decision":"block","reason":"blocked by glove"}`)
	}
	return b
}
