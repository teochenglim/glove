package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// TestAC_0_1_2_hook_block_exit_two — AC0.1.2: the hook path blocks
// `rm -rf /` with exit code 2 (belt-and-braces: even if stdout is lost,
// exit 2 blocks — PRD §6).
func TestAC_0_1_2_hook_block_exit_two(t *testing.T) {
	var out bytes.Buffer
	code := RunHook(&out, io.Discard, "claude-code",
		strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestAC_0_1_2_hook_stdout_verdict — AC0.1.2 other half: stdout carries the
// Claude Code decision envelope with a deny and the rule's reason.
func TestAC_0_1_2_hook_stdout_verdict(t *testing.T) {
	var out bytes.Buffer
	code := RunHook(&out, io.Discard, "claude-code",
		strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /"},"session_id":"s1","cwd":"/repo"}`))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the decision JSON: %v\n%s", err, out.String())
	}
	h := got.HookSpecificOutput
	if h.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", h.HookEventName)
	}
	if h.PermissionDecision != "deny" {
		t.Errorf("permissionDecision = %q, want deny", h.PermissionDecision)
	}
	if !strings.Contains(h.PermissionDecisionReason, "rm_root") {
		t.Errorf("reason = %q, want the rm_root rule reason", h.PermissionDecisionReason)
	}
}

// TestHookFixtureTable runs the shared hook fixture set
// (testdata/claude_hook_cases.json) through the real hook path.
func TestHookFixtureTable(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/claude_hook_cases.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var cases []struct {
		Name                   string `json:"name"`
		Input                  string `json:"input"`
		WantPermissionDecision string `json:"want_permission_decision"`
		WantExit               int    `json:"want_exit"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	if len(cases) < 8 {
		t.Fatalf("only %d fixture cases, want >= 8", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var out bytes.Buffer
			code := RunHook(&out, io.Discard, "claude-code", strings.NewReader(tc.Input))
			if code != tc.WantExit {
				t.Fatalf("exit = %d, want %d (stdout: %s)", code, tc.WantExit, out.String())
			}
			var got struct {
				HookSpecificOutput struct {
					PermissionDecision string `json:"permissionDecision"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("stdout not JSON: %v\n%s", err, out.String())
			}
			if got.HookSpecificOutput.PermissionDecision != tc.WantPermissionDecision {
				t.Errorf("decision = %q, want %q", got.HookSpecificOutput.PermissionDecision, tc.WantPermissionDecision)
			}
		})
	}
}

// TestHookFailsClosedOnBrokenStdin — PRD §8: internal errors never return
// "open"; unparseable stdin is a deny with exit 2.
func TestHookFailsClosedOnBrokenStdin(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := RunHook(&out, &errBuf, "claude-code", strings.NewReader(`{"tool_name":`))
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (fail closed)", code)
	}
	if !strings.Contains(out.String(), `"deny"`) {
		t.Errorf("stdout = %q, want a deny verdict", out.String())
	}
	if !strings.Contains(errBuf.String(), "invalid hook input") {
		t.Errorf("stderr = %q, want the failure reason", errBuf.String())
	}
}

// TestHookFailsClosedOnUnknownAgent — a misconfigured hook entry must never
// degrade into an open gate.
func TestHookFailsClosedOnUnknownAgent(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := RunHook(&out, &errBuf, "no-such-agent", strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (fail closed)", code)
	}
	if !strings.Contains(errBuf.String(), "unknown agent") {
		t.Errorf("stderr = %q, want the unknown-agent reason", errBuf.String())
	}
}
