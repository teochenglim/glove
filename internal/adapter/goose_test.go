package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/teochenglim/glove/pkg/contract"
)

// TestAC_0_10_1_conformance_goose — AC0.10.1: goose passes the shared
// conformance suite (the same phases as the 0.5.0 adapters).
func TestAC_0_10_1_conformance_goose(t *testing.T) {
	for _, spec := range conformSpecs() {
		if spec.name != GooseName {
			continue
		}
		t.Run("ac_0_10_1_conformance_goose", func(t *testing.T) {
			runConformanceSpec(t, spec)
		})
		return
	}
	t.Fatal("goose missing from conformSpecs")
}

// TestAC_0_10_1_interface_unchanged — AC0.10.1: the §5.1 interface is
// unchanged (the frozen method set) and goose resolves through the
// registry.
func TestAC_0_10_1_interface_unchanged(t *testing.T) {
	t.Run("ac_0_10_1_frozen_method_set", func(t *testing.T) {
		want := []string{"Detect", "HookEntry", "Install", "Name", "ParseHookInput", "RenderVerdict", "Uninstall"}
		typ := reflect.TypeFor[Adapter]()
		got := make([]string, 0, typ.NumMethod())
		for i := 0; i < typ.NumMethod(); i++ {
			got = append(got, typ.Method(i).Name)
		}
		if len(got) != len(want) {
			t.Fatalf("Adapter interface method set changed: %v", got)
		}
		for _, w := range want {
			found := false
			for _, g := range got {
				if g == w {
					found = true
				}
			}
			if !found {
				t.Errorf("Adapter interface lost %q", w)
			}
		}
	})
	t.Run("ac_0_10_1_registry_resolves", func(t *testing.T) {
		a, ok := For(GooseName)
		if !ok {
			t.Fatal("For(goose) does not resolve")
		}
		if a.Name() != GooseName || a.HookEntry() != hookEntryGoose {
			t.Fatalf("adapter = %s/%s", a.Name(), a.HookEntry())
		}
		found := false
		for _, n := range Names() {
			if n == GooseName {
				found = true
			}
		}
		if !found {
			t.Error("goose missing from Names()")
		}
	})
}

// TestAC_0_10_3_golden_fail_closed — AC0.10.3: the rendered hooks.json is
// byte-stable against the golden and carries the fail-closed posture
// (on_failure: "block", timeout tightened below goose's 30s default).
func TestAC_0_10_3_golden_fail_closed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agents", "plugins", "glove", "hooks", "hooks.json")
	a := NewGooseAt(target)
	if err := a.Install(t.Context(), InstallOptions{Writer: &bytes.Buffer{}}); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "conformance", "goose-hooks.json.golden"))
	if err != nil {
		t.Fatal(err)
	}
	// Both sides normalized (the 0.6.3 both-side CRLF lesson).
	norm := func(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }
	if norm(got) != norm(want) {
		t.Fatalf("rendered manifest drifted from the golden:\n--- got ---\n%s\n--- golden ---\n%s", got, want)
	}

	t.Run("ac_0_10_3_fail_closed_values", func(t *testing.T) {
		var manifest struct {
			Hooks struct {
				PreToolUse []struct {
					Hooks []struct {
						Type      string `json:"type"`
						Command   string `json:"command"`
						Timeout   int    `json:"timeout"`
						OnFailure string `json:"on_failure"`
					} `json:"hooks"`
				} `json:"PreToolUse"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(got, &manifest); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, rule := range manifest.Hooks.PreToolUse {
			for _, h := range rule.Hooks {
				if h.Command != hookEntryGoose {
					continue
				}
				found = true
				if h.OnFailure != "block" {
					t.Errorf("on_failure = %q, want block (fail-closed)", h.OnFailure)
				}
				if h.Timeout <= 0 || h.Timeout >= 30 {
					t.Errorf("timeout = %d, want tightened below goose's 30s default", h.Timeout)
				}
			}
		}
		if !found {
			t.Fatal("no glove command action under PreToolUse")
		}
	})
}

// TestAC_0_10_3_hooks_json_grammar_valid — AC0.10.3: the manifest stays
// inside goose's strict grammar — invalid values make goose skip the
// plugin's ENTIRE hooks.json with a warning, so the walker rejects unknown
// keys, unknown event names, bad action types, and invalid on_failure
// values (mirrors goose's parser expectations from the verified docs).
func TestAC_0_10_3_hooks_json_grammar_valid(t *testing.T) {
	raw := gooseHooksJSON(hookEntryGoose, gooseHookTimeoutSecs)
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if err := walkGooseGrammar(top); err != nil {
		t.Fatalf("manifest violates goose's grammar: %v", err)
	}
	// Negative: what goose would skip must be rejected by the walker too.
	bad := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{map[string]any{
						"type": "command", "command": hookEntryGoose,
						"timeout": 15, "on_failure": "carry-on",
					}},
				},
			},
		},
	}
	if err := walkGooseGrammar(bad); err == nil {
		t.Fatal("walker accepted on_failure value goose would skip the manifest for")
	}
}

// walkGooseGrammar checks the manifest against goose's documented grammar:
// top-level {hooks}; hooks keys ⊆ known event names; rule keys ⊆
// {matcher, hooks}; action keys ⊆ {type, command, timeout, on_failure};
// type == "command"; on_failure ∈ {"block"} (PreToolUse); matcher, when
// present, must compile as a regexp.
func walkGooseGrammar(top map[string]any) error {
	events := map[string]bool{
		"SessionStart": true, "SessionEnd": true, "Stop": true, "UserPromptSubmit": true,
		"PreToolUse": true, "PreToolUseResult": true, "PostToolUse": true, "PostToolUseFailure": true,
		"BeforeReadFile": true, "AfterFileEdit": true, "BeforeShellExecution": true, "AfterShellExecution": true,
	}
	for k := range top {
		if k != "hooks" {
			return errf("unknown top-level key %q", k)
		}
	}
	hooks, _ := top["hooks"].(map[string]any)
	for event, rulesAny := range hooks {
		if !events[event] {
			return errf("unknown event %q", event)
		}
		rules, ok := rulesAny.([]any)
		if !ok {
			return errf("event %q: rules must be a list", event)
		}
		for ri, rAny := range rules {
			rule, ok := rAny.(map[string]any)
			if !ok {
				return errf("%s[%d]: rule must be an object", event, ri)
			}
			for k := range rule {
				if k != "matcher" && k != "hooks" {
					return errf("%s[%d]: unknown rule key %q", event, ri, k)
				}
			}
			if m, present := rule["matcher"]; present {
				s, _ := m.(string)
				if _, err := regexp.Compile(s); err != nil {
					return errf("%s[%d]: matcher %q is not a valid regexp", event, ri, s)
				}
			}
			actions, ok := rule["hooks"].([]any)
			if !ok {
				return errf("%s[%d]: hooks must be a list", event, ri)
			}
			for ai, aAny := range actions {
				action, ok := aAny.(map[string]any)
				if !ok {
					return errf("%s[%d].hooks[%d]: action must be an object", event, ri, ai)
				}
				for k := range action {
					if k != "type" && k != "command" && k != "timeout" && k != "on_failure" {
						return errf("%s[%d].hooks[%d]: unknown action key %q", event, ri, ai, k)
					}
				}
				if t, _ := action["type"].(string); t != "command" {
					return errf("%s[%d].hooks[%d]: type %q is not command", event, ri, ai, t)
				}
				if c, _ := action["command"].(string); c == "" {
					return errf("%s[%d].hooks[%d]: command must not be empty", event, ri, ai)
				}
				switch of, _ := action["on_failure"].(string); of {
				case "", "block":
				default:
					return errf("%s[%d].hooks[%d]: on_failure %q is invalid (goose skips the manifest)", event, ri, ai, of)
				}
			}
		}
	}
	return nil
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// TestAC_0_10_4_review_renders_deny — AC0.10.4: review tightens to deny in
// goose's allow/block-only vocabulary, with the reason saying so; a
// manifest that lost on_failure: "block" is reported unhealthy.
func TestAC_0_10_4_review_renders_deny(t *testing.T) {
	a := NewGooseAt(filepath.Join(t.TempDir(), "hooks.json"))
	raw, err := a.RenderVerdict(contract.Verdict{
		Decision: contract.DecisionReview,
		Risk:     contract.RiskHigh,
		Reasons:  []string{"sudo: privilege escalation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got.Decision != "block" {
		t.Fatalf("review rendered as %q, want block (goose has no ask)", got.Decision)
	}
	if !strings.Contains(got.Reason, "sudo: privilege escalation") || !strings.Contains(got.Reason, "review rendered as deny") {
		t.Fatalf("review reason lost the story: %q", got.Reason)
	}

	// The health signal: a downgraded manifest (no on_failure: block) must
	// surface as UNHEALTHY — it silently reverts to goose's fail-open
	// default otherwise.
	t.Run("ac_0_10_4_downgraded_manifest_unhealthy", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "hooks.json")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		downgraded := strings.Replace(string(gooseHooksJSON(hookEntryGoose, gooseHookTimeoutSecs)),
			`"on_failure": "block"`, `"on_failure": "allow"`, 1)
		if err := os.WriteFile(target, []byte(downgraded), 0o644); err != nil {
			t.Fatal(err)
		}
		st := NewGooseAt(target).Status()
		if st.Problem == "" || !strings.Contains(st.Problem, "on_failure") {
			t.Fatalf("downgraded manifest must be unhealthy, got %+v", st)
		}
	})
}

// TestAC_0_10_4_decision_json_roundtrip — AC0.10.4: allow and block
// round-trip through goose's decision JSON; broken stdin is refused.
func TestAC_0_10_4_decision_json_roundtrip(t *testing.T) {
	a := NewGooseAt(filepath.Join(t.TempDir(), "hooks.json"))

	allow, err := a.RenderVerdict(contract.Verdict{Decision: contract.DecisionAllow})
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(allow, &d); err != nil {
		t.Fatalf("allow not JSON: %v", err)
	}
	if d.Decision != "allow" {
		t.Fatalf("allow rendered %q", d.Decision)
	}

	block, err := a.RenderVerdict(contract.Verdict{
		Decision: contract.DecisionBlock, Risk: contract.RiskCritical,
		Reasons: []string{"rm_root: recursive delete on /"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(block, &b); err != nil {
		t.Fatalf("block not JSON: %v", err)
	}
	if b.Decision != "block" || !strings.Contains(b.Reason, "rm_root") {
		t.Fatalf("block rendered %+v", b)
	}

	if _, err := a.ParseHookInput([]byte(`{"event":"PreToolUse"`)); err == nil {
		t.Fatal("truncated stdin must error")
	}
	if _, err := a.ParseHookInput([]byte(`{"event":"PreToolUse","session_id":"s"}`)); err == nil {
		t.Fatal("missing tool_name must error")
	}
}
