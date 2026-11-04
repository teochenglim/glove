// shim.go — machinery shared by the plugin-shim adapters (kilo, opencode:
// the OpenCode lineage, PRD §7 0.5.0). An embedded TypeScript template is
// rendered per agent and installed with the same guarantees as every other
// adapter install — diff before writing, backup of the prior state,
// idempotent, and it never overwrites a file it did not generate (PRD
// §10.4). The shim itself is a fixed protocol: it spawns `glove hook
// <agent>` with the tool call as JSON on stdin and reads the verdict from
// stdout, so all decisions flow through the same engine, policy and audit
// path as every other adapter.

package adapter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/teochenglim/glove/pkg/contract"
)

//go:embed templates/shim.ts.tmpl
var shimTemplateText string

// shimMarker identifies a file as glove-generated. Install refuses to touch
// a file without it; uninstall only removes glove's own files.
const shimMarker = "// glove shim for "

// shimEvent is the OpenCode-lineage hook event name (PRD §11).
const shimEvent = "tool.execute.before"

// shimData carries the per-agent render parameters. Header is the agent's
// plugin-API import line and Plugin the export shape — both verified
// against the agent's docs during 0.5.0 (PRD §11): Kilo requires a default
// module descriptor `{ id, server }`; OpenCode (V1 stable) accepts any
// exported plugin function. Both loaders run the shim under Bun
// in-process; `glove hook <agent>` is spawned per call.
type shimData struct {
	Agent     string
	Header    string
	HookEvent string
	Plugin    string
}

// shimHeaderFor returns the per-agent render parameters. One protocol
// change on either agent touches exactly this function (PRD §11).
func shimHeaderFor(agent string) (shimData, error) {
	switch agent {
	case KiloName:
		return shimData{
			Agent:     KiloName,
			Header:    `import type { Plugin } from "@kilocode/plugin";`,
			HookEvent: shimEvent,
			Plugin: `const glovePlugin: Plugin = async () => ({
  "` + shimEvent + `": gloveGuard,
});

export default { id: "glove", server: glovePlugin };`,
		}, nil
	case OpenCodeName:
		return shimData{
			Agent:     OpenCodeName,
			Header:    `import type { Plugin } from "@opencode-ai/plugin";`,
			HookEvent: shimEvent,
			Plugin: `export const GlovePlugin: Plugin = async () => ({
  "` + shimEvent + `": gloveGuard,
});`,
		}, nil
	}
	return shimData{}, fmt.Errorf("no shim template for agent %q", agent)
}

func renderShim(data shimData) ([]byte, error) {
	tmpl, err := template.New("shim").Parse(shimTemplateText)
	if err != nil {
		return nil, fmt.Errorf("parse shim template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render shim: %w", err)
	}
	return buf.Bytes(), nil
}

// installShim writes the rendered shim to path with the full install
// contract: diff first, nothing written under DryRun, backup of any
// replaced prior content, and a refusal to overwrite a foreign file.
func installShim(ctx context.Context, path string, content []byte, agent string, opts InstallOptions) error {
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
	if len(original) > 0 && !bytes.Contains(original, []byte(shimMarker)) {
		// AC0.5.1 broken-config guard, shim flavour: the file belongs to
		// someone else — glove must not silently replace it.
		return fmt.Errorf("%s exists and is not a glove shim — refusing to overwrite; move it aside or point install at another path", target)
	}
	if bytes.Equal(original, content) {
		fmt.Fprintf(w, "%s: glove shim already installed — no changes\n", target)
		return nil
	}

	fmt.Fprintf(w, "glove install %s → %s\n", agent, target)
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
	// 0644: the shim carries no secrets (unlike settings files) and is read
	// back by the agent's plugin loader.
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

// uninstallShim removes a glove-generated shim file. Idempotent; refuses to
// delete a file it did not write. The prior content is backed up beside the
// removal point, mirroring the uninstall contract of the settings adapters.
func uninstallShim(ctx context.Context, path string, w io.Writer) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(w, "%s: no glove shim present — nothing to do\n", path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if !bytes.Contains(raw, []byte(shimMarker)) {
		return fmt.Errorf("%s is not a glove shim — leaving it alone", path)
	}
	if err := os.WriteFile(path+".glove.bak", raw, 0o644); err != nil {
		return fmt.Errorf("write backup %s.glove.bak: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	fmt.Fprintf(w, "removed %s (backup: %s)\n", path, path+".glove.bak")
	return nil
}

// shimStatus builds the AgentStatus for a shim-based adapter: healthy when
// the rendered-shim file is present and intact, unhealthy when it exists
// but was corrupted or replaced (AC0.5.4's corrupted-config detection).
func shimStatus(path, agent, hookEntry string) AgentStatus {
	st := AgentStatus{
		Name:       agent,
		Mode:       ModeHook,
		ConfigPath: path,
		Entry:      hookEntry,
	}
	st.Detected, st.ConfigPath, _ = detectShim(path)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st // healthy, just not installed
	}
	if err != nil {
		st.Problem = fmt.Sprintf("read shim: %v", err)
		return st
	}
	if !bytes.Contains(raw, []byte(shimMarker)) {
		st.Problem = "plugin file exists but is not a glove shim — refusing to manage it"
		return st
	}
	data, err := shimHeaderFor(agent)
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	want, err := renderShim(data)
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	if !bytes.Equal(raw, want) {
		st.Problem = "glove shim is present but modified — re-run `glove install " + agent + "` to regenerate it"
		return st
	}
	st.HookInstalled = true
	return st
}

// detectShim reports the agent as installed when the shim file or the
// agent's config root exists.
func detectShim(path string) (bool, string, error) {
	if _, err := os.Stat(path); err == nil {
		return true, path, nil
	}
	dir := filepath.Dir(filepath.Dir(path)) // …/<agent>/plugin/glove.ts → …/<agent>
	if _, err := os.Stat(dir); err == nil {
		return true, path, nil
	}
	return false, path, nil
}

// shimVerdict is the shim-protocol verdict: what `glove hook <agent>`
// prints and the shim parses (the only shim consumer is our own generated
// TypeScript, so the shape stays deliberately small).
type shimVerdict struct {
	Decision      string   `json:"decision"`
	Risk          string   `json:"risk"`
	Confidence    float64  `json:"confidence"`
	Deterministic bool     `json:"deterministic"`
	Reasons       []string `json:"reasons"`
}

func renderShimVerdict(v contract.Verdict) ([]byte, error) {
	out := shimVerdict{
		Decision:      string(v.Decision),
		Risk:          string(v.Risk),
		Confidence:    v.Confidence,
		Deterministic: v.Deterministic,
		Reasons:       v.Reasons,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func parseShimInput(agent string, raw []byte) (HookInput, error) {
	var wire struct {
		Tool      string          `json:"tool"`
		Args      json.RawMessage `json:"args"`
		SessionID string          `json:"session_id"`
		CallID    string          `json:"call_id"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return HookInput{}, fmt.Errorf("parse %s shim input: %w", agent, err)
	}
	if strings.TrimSpace(wire.Tool) == "" {
		return HookInput{}, fmt.Errorf("parse %s shim input: missing \"tool\"", agent)
	}
	if len(wire.Args) == 0 {
		wire.Args = json.RawMessage(`{}`)
	}
	return HookInput{
		Agent:     agent,
		Event:     shimEvent,
		ToolName:  wire.Tool,
		ToolInput: wire.Args,
		SessionID: wire.SessionID,
		CWD:       "", // the shim does not know the workspace; the engine does not need it
	}, nil
}
