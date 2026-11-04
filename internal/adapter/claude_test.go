package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	if _, ok := For("claude-code"); !ok {
		t.Error("claude-code must resolve")
	}
	if _, ok := For("kilo"); ok {
		t.Error("kilo must not resolve before v0.5.0")
	}
}
