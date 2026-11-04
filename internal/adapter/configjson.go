// configjson.go — the shared install/uninstall/status contract for adapters
// whose agent keeps a JSON config file (claude-code settings.json, codex
// hooks.json, cursor hooks.json, gemini settings.json). Every install shows
// a diff before writing, backs up the prior state, is idempotent, and
// refuses to touch an invalid config (PRD §10.4, §7 0.5.0).

package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// jsonMutate inserts (or detects) the agent's glove hook entry in a decoded
// config. present = the entry is already installed; changed = the config
// was modified and needs writing back.
type jsonMutate func(cfg map[string]any) (present, changed bool, err error)

// jsonNumber renders an int as a JSON number without float artifacts,
// matching the json.Number values the decoder produces for user configs.
func jsonNumber(n int) json.Number { return json.Number(strconv.Itoa(n)) }

// --- JSON config + diff helpers shared by every config-file adapter ---

func marshalSettings(cfg map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteDiff prints the compact before/after line diff used by every
// install. Exported for the CLI's `glove list --matrix --check` drift
// report — the same review surface installs show.
func WriteDiff(w io.Writer, oldLines, newLines []string) { writeDiff(w, oldLines, newLines) }

// SplitLines splits s into diff lines (trailing newline trimmed).
func SplitLines(s string) []string { return splitLines(s) }

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// writeDiff prints a compact before/after diff: removals first, then
// additions, each in document order. Config diffs are localized (one hook
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

// installJSONHook implements the shared JSON-config install contract:
// read-modify-write with a diff, dry-run guard, backup, idempotency, and a
// loud refusal to touch an invalid config — nothing written, original
// untouched (AC0.1.4 semantics, generalized to the 0.5.0 matrix).
func installJSONHook(path, agent string, opts InstallOptions, mutate jsonMutate) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	target := path
	if opts.ConfigPath != "" {
		target = opts.ConfigPath
	}

	var original []byte
	if raw, err := os.ReadFile(target); err == nil {
		original = raw
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", target, err)
	}

	cfg := map[string]any{}
	if len(original) > 0 {
		dec := json.NewDecoder(bytes.NewReader(original))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			// Fail loudly, touch nothing, no backup — the user must not
			// discover their config was rewritten by glove.
			return fmt.Errorf("%s is not valid JSON: %w — nothing written, original untouched", target, err)
		}
	}

	present, changed, err := mutate(cfg)
	if err != nil {
		return err
	}
	if present {
		fmt.Fprintf(w, "%s: glove hook already installed — no changes\n", target)
		return nil
	}
	if !changed {
		return fmt.Errorf("%s: hook insertion produced no change — aborting", target)
	}

	newJSON, err := marshalSettings(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	fmt.Fprintf(w, "glove install %s → %s\n", agent, target)
	writeDiff(w, splitLines(string(original)), splitLines(string(newJSON)))
	if opts.DryRun {
		fmt.Fprintln(w, "dry run — nothing written")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	// 0600 across the matrix: these configs can hold unrelated (sometimes
	// secret-bearing) agent settings; glove keeps them as private as it
	// found them.
	if err := os.WriteFile(target+".glove.bak", original, 0o600); err != nil {
		return fmt.Errorf("write backup %s.glove.bak: %w", target, err)
	}
	if err := os.WriteFile(target, newJSON, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Fprintf(w, "installed (backup: %s.glove.bak)\n", target)
	return nil
}

// removeJSONHook strips every glove hook entry under cfg["hooks"][event],
// backing up the prior file. Idempotent: nothing installed → no-op. It
// handles both array shapes in the matrix: wrapped groups
// {"hooks":[{type,command,...}]} (claude-code, codex, gemini) and flat
// entries {command,...} (cursor).
func removeJSONHook(path, event, entry string) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	cfg := map[string]any{}
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w — nothing written", path, err)
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	entries, _ := hooks[event].([]any)
	kept := make([]any, 0, len(entries))
	for _, g := range entries {
		gm, ok := g.(map[string]any)
		if !ok {
			kept = append(kept, g)
			continue
		}
		if inner, isGroup := gm["hooks"].([]any); isGroup {
			keptInner := make([]any, 0, len(inner))
			for _, h := range inner {
				if hm, ok := h.(map[string]any); ok {
					if cmd, _ := hm["command"].(string); cmd == entry {
						continue
					}
				}
				keptInner = append(keptInner, h)
			}
			if len(keptInner) == 0 {
				continue // group existed only for glove
			}
			gm["hooks"] = keptInner
			kept = append(kept, gm)
			continue
		}
		if cmd, _ := gm["command"].(string); cmd == entry {
			continue
		}
		kept = append(kept, gm)
	}
	hooks[event] = kept
	newJSON, err := marshalSettings(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".glove.bak", raw, 0o600); err != nil {
		return fmt.Errorf("write backup %s.glove.bak: %w", path, err)
	}
	return os.WriteFile(path, newJSON, 0o600)
}

// statusJSONHook builds the AgentStatus for a JSON-config adapter. A
// present-but-unparseable config is a Problem (AC0.5.4's corrupted-config
// detection); a valid config without the entry is healthy-but-uninstalled.
// inspect, when non-nil, sees the decoded config after the presence check
// and may flag degradations (e.g. cursor's failClosed).
func statusJSONHook(a Adapter, path string, present func(cfg map[string]any) bool, inspect func(st *AgentStatus, cfg map[string]any), detectErr error) AgentStatus {
	st := AgentStatus{
		Name:       a.Name(),
		Mode:       ModeHook,
		ConfigPath: path,
		Entry:      a.HookEntry(),
	}
	if detectErr != nil {
		st.Problem = detectErr.Error()
		return st
	}
	if installed, cfgPath, err := a.Detect(); err == nil {
		st.Detected, st.ConfigPath = installed, cfgPath
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st
	}
	if err != nil {
		st.Problem = fmt.Sprintf("read config: %v", err)
		return st
	}
	var cfg map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		st.Problem = fmt.Sprintf("config file is not valid JSON: %v", err)
		return st
	}
	if present(cfg) {
		st.HookInstalled = true
	}
	if inspect != nil {
		inspect(&st, cfg)
	}
	return st
}

// hasGloveHookIn reports whether a glove hook entry with the exact command
// exists under cfg["hooks"][event] — in wrapped groups
// {"hooks":[{type,command}]} (claude-code, codex, gemini).
func hasGloveHookIn(cfg map[string]any, event, entry string) bool {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	groups, _ := hooks[event].([]any)
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); cmd == entry {
				return true
			}
		}
	}
	return false
}

// detectConfigDir reports the agent as installed when its config directory
// or its binary exists — the generic Detect for config-file adapters.
func detectConfigDir(configPath, binary string) (bool, string, error) {
	dir := filepath.Dir(configPath)
	if _, err := os.Stat(dir); err == nil {
		return true, configPath, nil
	}
	if p, err := exec.LookPath(binary); err == nil && p != "" {
		return true, configPath, nil
	}
	return false, configPath, nil
}
