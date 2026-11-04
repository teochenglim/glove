package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/teochenglim/glove/pkg/contract"
)

func settingsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings.json")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func install(t *testing.T, path string) string {
	t.Helper()
	var out bytes.Buffer
	a := NewClaudeAt(path)
	if err := a.Install(context.Background(), InstallOptions{Writer: &out}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return out.String()
}

// TestAC_0_1_3_install_idempotent — AC0.1.3: installing twice leaves the
// settings byte-identical to one install.
func TestAC_0_1_3_install_idempotent(t *testing.T) {
	path := settingsPath(t)
	install(t, path)
	afterFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first install: %v", err)
	}
	out := install(t, path)
	afterSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second install: %v", err)
	}
	if !bytes.Equal(afterFirst, afterSecond) {
		t.Fatalf("second install changed the file:\nfirst:\n%s\nsecond:\n%s", afterFirst, afterSecond)
	}
	if !strings.Contains(out, "already installed") {
		t.Errorf("second install output = %q, want an already-installed notice", out)
	}
}

// TestAC_0_1_3_install_single_hook_entry — AC0.1.3: no duplicate hook group
// even across repeated installs.
func TestAC_0_1_3_install_single_hook_entry(t *testing.T) {
	path := settingsPath(t)
	install(t, path)
	install(t, path)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode installed settings: %v", err)
	}
	if n := len(cfg.Hooks.PreToolUse); n != 1 {
		t.Fatalf("PreToolUse groups = %d, want exactly 1", n)
	}
	g := cfg.Hooks.PreToolUse[0]
	if len(g.Hooks) != 1 || g.Hooks[0].Command != "glove hook claude-code" || g.Hooks[0].Type != "command" {
		t.Errorf("hook group = %+v, want one command hook %q", g, "glove hook claude-code")
	}
}

// TestAC_0_1_4_install_rejects_invalid_json — AC0.1.4: a broken settings
// file makes install fail with a clear error.
func TestAC_0_1_4_install_rejects_invalid_json(t *testing.T) {
	path := settingsPath(t)
	mustWrite(t, path, `{"hooks": {broken`)

	a := NewClaudeAt(path)
	err := a.Install(context.Background(), InstallOptions{Writer: io.Discard})
	if err == nil {
		t.Fatal("install must fail on invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %v, want a clear invalid-JSON message", err)
	}
}

// TestAC_0_1_4_install_preserves_config — AC0.1.4: the failed install must
// leave the original byte-identical and must not write a backup.
func TestAC_0_1_4_install_preserves_config(t *testing.T) {
	path := settingsPath(t)
	original := "{not json at all\nreally broken"
	mustWrite(t, path, original)

	a := NewClaudeAt(path)
	_ = a.Install(context.Background(), InstallOptions{Writer: io.Discard})

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != original {
		t.Fatalf("original modified:\nwant: %q\ngot:  %q", original, got)
	}
	if _, err := os.Stat(path + ".glove.bak"); !os.IsNotExist(err) {
		t.Error("backup must not be written for a failed install")
	}
}

// TestInstallShowsDiffAndBacksUp — an install must show a diff (PRD §10.4)
// and back up the prior state before the first write.
func TestInstallShowsDiffAndBacksUp(t *testing.T) {
	path := settingsPath(t)
	original := "{\n  \"model\": \"opus\"\n}\n"
	mustWrite(t, path, original)

	var out bytes.Buffer
	a := NewClaudeAt(path)
	if err := a.Install(context.Background(), InstallOptions{Writer: &out}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out.String(), "+") {
		t.Errorf("install output must show the diff, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "glove hook claude-code") {
		t.Errorf("install output must show the hook command, got:\n%s", out.String())
	}
	bak, err := os.ReadFile(path + ".glove.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bak) != original {
		t.Errorf("backup = %q, want the pre-install content %q", bak, original)
	}
}

// TestInstallDryRunWritesNothing — --dry-run shows the diff only.
func TestInstallDryRunWritesNothing(t *testing.T) {
	path := settingsPath(t)
	var out bytes.Buffer
	a := NewClaudeAt(path)
	if err := a.Install(context.Background(), InstallOptions{DryRun: true, Writer: &out}); err != nil {
		t.Fatalf("dry-run install: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dry run must not create the settings file")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("output = %q, want a dry-run notice", out.String())
	}
}

// TestUninstallIdempotent covers the interface's other half.
func TestUninstallIdempotent(t *testing.T) {
	path := settingsPath(t)
	a := NewClaudeAt(path)
	install(t, path)
	if err := a.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if hasGloveHook(mustDecode(t, raw), hookEntryFmt) {
		t.Error("hook still present after uninstall")
	}
	if err := a.Uninstall(context.Background()); err != nil {
		t.Fatalf("second uninstall must be a no-op, got %v", err)
	}
}

func mustDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return cfg
}

func TestRenderVerdictTable(t *testing.T) {
	a := NewClaudeAt(settingsPath(t))
	t.Run("table", func(t *testing.T) {
		cases := []struct {
			decision contract.Decision
			want     string
		}{
			{contract.DecisionAllow, "allow"},
			{contract.DecisionReview, "ask"},
			{contract.DecisionBlock, "deny"},
		}
		for _, tc := range cases {
			v := contract.Verdict{
				Decision: tc.decision,
				Risk:     contract.RiskHigh,
				Reasons:  []string{"sudo: privilege escalation"},
			}
			raw, err := a.RenderVerdict(v)
			if err != nil {
				t.Fatalf("render %s: %v", tc.decision, err)
			}
			var got hookOut
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode render %s: %v", tc.decision, err)
			}
			h := got.HookSpecificOutput
			if h.HookEventName != "PreToolUse" {
				t.Errorf("%s: hookEventName = %q", tc.decision, h.HookEventName)
			}
			if h.PermissionDecision != tc.want {
				t.Errorf("%s: permissionDecision = %q, want %q", tc.decision, h.PermissionDecision, tc.want)
			}
			if tc.decision == contract.DecisionBlock && !strings.Contains(h.PermissionDecisionReason, "sudo") {
				t.Errorf("%s: reason = %q, want the rule reason", tc.decision, h.PermissionDecisionReason)
			}
		}
	})
}

func TestParseHookInput(t *testing.T) {
	a := NewClaudeAt(settingsPath(t))
	t.Run("full_payload", func(t *testing.T) {
		in, err := a.ParseHookInput([]byte(`{"session_id":"s1","transcript_path":"/t.jsonl","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if in.Agent != "claude-code" || in.Event != "PreToolUse" || in.ToolName != "Bash" || in.SessionID != "s1" || in.CWD != "/repo" {
			t.Errorf("parsed = %+v", in)
		}
		if !strings.Contains(string(in.ToolInput), "ls") {
			t.Errorf("tool_input = %s", in.ToolInput)
		}
	})
	t.Run("minimal_payload", func(t *testing.T) {
		in, err := a.ParseHookInput([]byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if in.ToolName != "Bash" {
			t.Errorf("tool_name = %q", in.ToolName)
		}
	})
	t.Run("broken_payload_errors", func(t *testing.T) {
		if _, err := a.ParseHookInput([]byte(`{`)); err == nil {
			t.Fatal("broken stdin JSON must error")
		}
	})
}

func TestForRegistry(t *testing.T) {
	for _, name := range Names() {
		if _, ok := For(name); !ok {
			t.Errorf("%s listed in Names() but does not resolve via For()", name)
		}
	}
	for _, name := range []string{"claude-code", "kilo", "codex", "opencode", "cursor", "gemini", "zed"} {
		if _, ok := For(name); !ok {
			t.Errorf("%s must resolve (v0.5.0 adapter matrix)", name)
		}
	}
	if _, ok := For("no-such-agent"); ok {
		t.Error("unknown agents must not resolve")
	}
}

// ====================================================================
// v0.5.0 conformance suite (PRD §7 0.5.0, PRD-0.5.0-1): the shared
// harness every adapter must pass — install diff/backup/idempotency,
// dry-run guard, broken-config refusal, uninstall idempotency,
// ParseHookInput round-trips from per-agent fixtures, RenderVerdict in
// the agent's native vocabulary, registry membership and Status health.
// New adapters land by adding a conformSpec row — the suite is the gate.

// Compile-time interface freeze: every adapter implements the §5.1
// interface unchanged from 0.1.0. A signature change breaks this list,
// not just the reflection check below.
var (
	_ Adapter = (*Claude)(nil)
	_ Adapter = (*Kilo)(nil)
	_ Adapter = (*Codex)(nil)
	_ Adapter = (*OpenCode)(nil)
	_ Adapter = (*Cursor)(nil)
	_ Adapter = (*Gemini)(nil)
	_ Adapter = (*Zed)(nil)
)

// conformFlavor distinguishes the install/corruption mechanics: json
// adapters carry real user config (invalid JSON is the corruption case),
// shim adapters own their whole file (foreign content is the refusal
// case), and the audit adapter writes nothing at all.
type conformFlavor string

const (
	flavorJSON  conformFlavor = "json"
	flavorShim  conformFlavor = "shim"
	flavorAudit conformFlavor = "audit"
)

type conformSpec struct {
	name   string
	flavor conformFlavor
	newAt  func(dir string) Adapter
	stdin  string // testdata/conformance/<name>.stdin.json
	tool   string // expected ToolName after ParseHookInput
	render func(t *testing.T, d contract.Decision, raw []byte)
}

func claudeStyleRender(t *testing.T, d contract.Decision, raw []byte) {
	t.Helper()
	if d == contract.DecisionAllow && len(raw) == 0 {
		t.Fatal("claude-style allow must render a decision envelope, got empty output")
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("render %s: not JSON: %v\n%s", d, err, raw)
	}
	if got.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("render %s: hookEventName = %q", d, got.HookSpecificOutput.HookEventName)
	}
	want := map[contract.Decision]string{
		contract.DecisionAllow:  "allow",
		contract.DecisionReview: "ask",
		contract.DecisionBlock:  "deny",
	}[d]
	if got.HookSpecificOutput.PermissionDecision != want {
		t.Errorf("render %s: permissionDecision = %q, want %q", d, got.HookSpecificOutput.PermissionDecision, want)
	}
}

func cursorRender(t *testing.T, d contract.Decision, raw []byte) {
	t.Helper()
	var got struct {
		Permission string `json:"permission"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("render %s: not JSON: %v", d, err)
	}
	want := map[contract.Decision]string{
		contract.DecisionAllow:  "allow",
		contract.DecisionReview: "deny", // tighten-only: cursor has no enforced ask
		contract.DecisionBlock:  "deny",
	}[d]
	if got.Permission != want {
		t.Errorf("render %s: permission = %q, want %q", d, got.Permission, want)
	}
}

func geminiRender(t *testing.T, d contract.Decision, raw []byte) {
	t.Helper()
	if d == contract.DecisionAllow {
		if len(raw) != 0 {
			t.Errorf("gemini allow must render empty stdout (non-JSON stdout fails open on gemini), got %q", raw)
		}
		return
	}
	var got struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("render %s: not JSON: %v", d, err)
	}
	if got.Decision != "deny" {
		t.Errorf("render %s: decision = %q, want deny", d, got.Decision)
	}
	if got.Reason == "" {
		t.Errorf("render %s: reason must not be empty", d)
	}
}

func shimRender(t *testing.T, d contract.Decision, raw []byte) {
	t.Helper()
	var got struct {
		Decision string   `json:"decision"`
		Reasons  []string `json:"reasons"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("render %s: not JSON: %v", d, err)
	}
	want := map[contract.Decision]string{
		contract.DecisionAllow:  "allow",
		contract.DecisionReview: "review",
		contract.DecisionBlock:  "block",
	}[d]
	if got.Decision != want {
		t.Errorf("render %s: decision = %q, want %q", d, got.Decision, want)
	}
	if d == contract.DecisionBlock && len(got.Reasons) == 0 {
		t.Errorf("render %s: block must carry reasons", d)
	}
}

func conformSpecs() []conformSpec {
	return []conformSpec{
		{name: "claude-code", flavor: flavorJSON,
			newAt: func(dir string) Adapter { return NewClaudeAt(filepath.Join(dir, "settings.json")) },
			stdin: "claude-code.stdin.json", tool: "Bash", render: claudeStyleRender},
		{name: "kilo", flavor: flavorShim,
			newAt: func(dir string) Adapter { return NewKiloAt(filepath.Join(dir, "kilo", "plugin", "glove.ts")) },
			stdin: "kilo.stdin.json", tool: "bash", render: shimRender},
		{name: "codex", flavor: flavorJSON,
			newAt: func(dir string) Adapter { return NewCodexAt(filepath.Join(dir, "codex", "hooks.json")) },
			stdin: "codex.stdin.json", tool: "Bash", render: claudeStyleRender},
		{name: "opencode", flavor: flavorShim,
			newAt: func(dir string) Adapter { return NewOpenCodeAt(filepath.Join(dir, "opencode", "plugin", "glove.ts")) },
			stdin: "opencode.stdin.json", tool: "bash", render: shimRender},
		{name: "cursor", flavor: flavorJSON,
			newAt: func(dir string) Adapter { return NewCursorAt(filepath.Join(dir, "cursor", "hooks.json")) },
			stdin: "cursor.stdin.json", tool: "Bash", render: cursorRender},
		{name: "gemini", flavor: flavorJSON,
			newAt: func(dir string) Adapter { return NewGeminiAt(filepath.Join(dir, "gemini", "settings.json")) },
			stdin: "gemini.stdin.json", tool: "run_shell_command", render: geminiRender},
		{name: "zed", flavor: flavorAudit,
			newAt: func(dir string) Adapter { return NewZed() },
			stdin: "zed.stdin.json", tool: "terminal", render: shimRender},
	}
}

// TestAC_0_5_1_conformance_all_adapters — AC0.5.1: all five new agents
// (plus zed's render/parse/registry half) pass the shared conformance
// suite; the phases below are the gate every future adapter lands behind.
func TestAC_0_5_1_conformance_all_adapters(t *testing.T) {
	for _, spec := range conformSpecs() {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			dir := t.TempDir()
			a := spec.newAt(dir)

			// --- registry ---
			t.Run("ac_0_5_1_registry", func(t *testing.T) {
				if a.Name() != spec.name {
					t.Errorf("Name() = %q, want %q", a.Name(), spec.name)
				}
				if a.HookEntry() == "" {
					t.Error("HookEntry() must not be empty")
				}
				if _, ok := For(a.Name()); !ok {
					t.Errorf("For(%q) does not resolve", a.Name())
				}
			})

			// --- parse: the agent's native stdin fixture ---
			t.Run("ac_0_5_1_parse_fixture", func(t *testing.T) {
				raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "conformance", spec.stdin))
				if err != nil {
					t.Fatalf("read fixture %s: %v", spec.stdin, err)
				}
				in, err := a.ParseHookInput(raw)
				if err != nil {
					t.Fatalf("parse fixture: %v", err)
				}
				if in.Agent != spec.name {
					t.Errorf("Agent = %q, want %q", in.Agent, spec.name)
				}
				if in.ToolName != spec.tool {
					t.Errorf("ToolName = %q, want %q", in.ToolName, spec.tool)
				}
				if !strings.Contains(string(in.ToolInput), "ls -la") {
					t.Errorf("ToolInput = %s, want the command text", in.ToolInput)
				}
				if _, err := a.ParseHookInput([]byte(`{`)); err == nil {
					t.Error("broken stdin JSON must error")
				}
			})

			// --- render: the agent's native decision vocabulary ---
			t.Run("ac_0_5_1_render_native", func(t *testing.T) {
				for _, d := range []contract.Decision{contract.DecisionAllow, contract.DecisionReview, contract.DecisionBlock} {
					raw, err := a.RenderVerdict(contract.Verdict{
						Decision: d,
						Risk:     contract.RiskHigh,
						Reasons:  []string{"rm_root: recursive delete on /"},
					})
					if err != nil {
						t.Fatalf("render %s: %v", d, err)
					}
					spec.render(t, d, raw)
				}
			})

			if spec.flavor == flavorAudit {
				// zed: audit mode installs nothing, uninstall is a no-op,
				// status reports the audit posture as healthy.
				t.Run("ac_0_5_1_audit_mode", func(t *testing.T) {
					var out bytes.Buffer
					if err := a.Install(context.Background(), InstallOptions{Writer: &out}); err != nil {
						t.Fatalf("install: %v", err)
					}
					if !strings.Contains(out.String(), "audit mode") {
						t.Errorf("install output = %q, want the audit-mode statement", out.String())
					}
					if err := a.Uninstall(context.Background()); err != nil {
						t.Fatalf("uninstall: %v", err)
					}
					st := StatusOf(a)
					if st.Mode != ModeAudit {
						t.Errorf("Mode = %q, want audit", st.Mode)
					}
					if st.HookInstalled {
						t.Error("audit adapter must never report a hook installed")
					}
					if st.Problem != "" {
						t.Errorf("audit posture must be healthy, got problem %q", st.Problem)
					}
				})
				return // audit adapter: no config install phases
			}

			// --- install: diff, backup, idempotency ---
			t.Run("ac_0_5_1_install", func(t *testing.T) {
				var out bytes.Buffer
				if err := a.Install(context.Background(), InstallOptions{Writer: &out}); err != nil {
					t.Fatalf("install: %v", err)
				}
				if !strings.Contains(out.String(), "+") {
					t.Errorf("install output must show the diff, got:\n%s", out.String())
				}
				if !strings.Contains(out.String(), a.HookEntry()) {
					t.Errorf("install output must show the hook entry, got:\n%s", out.String())
				}
				st := StatusOf(a)
				if !st.HookInstalled || st.Problem != "" {
					t.Errorf("status after install = %+v, want installed and healthy", st)
				}
			})

			// --- idempotency: second install changes nothing ---
			t.Run("ac_0_5_1_install_idempotent", func(t *testing.T) {
				target := spec.targetPath(dir)
				first, err := os.ReadFile(target)
				if err != nil {
					t.Fatalf("read after first install: %v", err)
				}
				var out bytes.Buffer
				if err := a.Install(context.Background(), InstallOptions{Writer: &out}); err != nil {
					t.Fatalf("second install: %v", err)
				}
				if !strings.Contains(out.String(), "already installed") {
					t.Errorf("second install output = %q, want an already-installed notice", out.String())
				}
				second, err := os.ReadFile(target)
				if err != nil {
					t.Fatalf("read after second install: %v", err)
				}
				if !bytes.Equal(first, second) {
					t.Fatal("second install changed the file")
				}
			})

			// --- dry run: shows the diff, writes nothing ---
			t.Run("ac_0_5_1_dry_run", func(t *testing.T) {
				fresh := spec.newAt(filepath.Join(dir, "dryrun"))
				var out bytes.Buffer
				if err := fresh.Install(context.Background(), InstallOptions{DryRun: true, Writer: &out}); err != nil {
					t.Fatalf("dry-run install: %v", err)
				}
				if !strings.Contains(out.String(), "dry run") {
					t.Errorf("output = %q, want a dry-run notice", out.String())
				}
				if target := spec.targetPath(filepath.Join(dir, "dryrun")); target != "" {
					if _, err := os.Stat(target); !os.IsNotExist(err) {
						t.Errorf("dry run must not create %s", target)
					}
				}
			})

			// --- corruption guard: refuses and preserves ---
			t.Run("ac_0_5_1_corrupt_guard", func(t *testing.T) {
				fresh := spec.newAt(filepath.Join(dir, "corrupt"))
				target := spec.targetPath(filepath.Join(dir, "corrupt"))
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				corrupt := "{broken json"
				if spec.flavor == flavorShim {
					corrupt = "// someone else's plugin file"
				}
				if err := os.WriteFile(target, []byte(corrupt), 0o600); err != nil {
					t.Fatalf("write corrupt: %v", err)
				}
				err := fresh.Install(context.Background(), InstallOptions{Writer: io.Discard})
				if err == nil {
					t.Fatal("install must refuse a foreign/broken file")
				}
				want := "not a glove shim"
				if spec.flavor == flavorJSON {
					want = "not valid JSON"
				}
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want it to mention %q", err, want)
				}
				got, rerr := os.ReadFile(target)
				if rerr != nil || string(got) != corrupt {
					t.Errorf("corrupt file modified by install: %q (%v)", got, rerr)
				}
				if _, serr := os.Stat(target + ".glove.bak"); !os.IsNotExist(serr) {
					t.Error("no backup must be written for a refused install")
				}
			})

			// --- uninstall: removes, idempotent ---
			t.Run("ac_0_5_1_uninstall", func(t *testing.T) {
				if err := a.Uninstall(context.Background()); err != nil {
					t.Fatalf("uninstall: %v", err)
				}
				if st := StatusOf(a); st.HookInstalled {
					t.Errorf("hook still present after uninstall: %+v", st)
				}
				if err := a.Uninstall(context.Background()); err != nil {
					t.Fatalf("second uninstall must be a no-op, got %v", err)
				}
			})
		})
	}
}

// targetPath returns the file the adapter writes for a given spec dir —
// the explicit path for pinned constructors, zed writes nothing.
func (s conformSpec) targetPath(dir string) string {
	switch s.name {
	case "claude-code":
		return filepath.Join(dir, "settings.json")
	case "kilo":
		return filepath.Join(dir, "kilo", "plugin", "glove.ts")
	case "codex":
		return filepath.Join(dir, "codex", "hooks.json")
	case "opencode":
		return filepath.Join(dir, "opencode", "plugin", "glove.ts")
	case "cursor":
		return filepath.Join(dir, "cursor", "hooks.json")
	case "gemini":
		return filepath.Join(dir, "gemini", "settings.json")
	default:
		return "" // zed: audit mode writes nothing
	}
}

// TestAC_0_5_1_adapter_interface_frozen — AC0.5.1 other half: the Adapter
// interface method set is unchanged from 0.1.0 (PRD §5.1). Additive
// optional interfaces (StatusReporter) live beside it, never inside it.
func TestAC_0_5_1_adapter_interface_frozen(t *testing.T) {
	want := []string{
		"Detect",
		"HookEntry",
		"Install",
		"Name",
		"ParseHookInput",
		"RenderVerdict",
		"Uninstall",
	}
	sort.Strings(want)
	typ := reflect.TypeFor[Adapter]()
	got := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Adapter interface changed since 0.1.0:\n got %v\nwant %v\n— a schema-version bump and owner sign-off are required (PRD §5.1, ADR 2)", got, want)
	}
}
