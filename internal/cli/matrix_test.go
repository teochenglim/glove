package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/audit"
)

// matrixTestEnv pins HOME (adapter config paths resolve under it) on top of
// the offline policy env — the matrix tests exercise the real install /
// list / doctor surfaces without touching the user's machine.
func matrixTestEnv(t *testing.T) (home string) {
	t.Helper()
	hookTestEnv(t)
	home = t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// fixtureStdin loads the per-agent native stdin fixture from
// testdata/conformance/.
func fixtureStdin(t *testing.T, agent string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "conformance", agent+".stdin.json"))
	if err != nil {
		t.Fatalf("read fixture for %s: %v", agent, err)
	}
	return string(raw)
}

// installViaCLI runs `glove install <agent>` through the real root command.
func installViaCLI(t *testing.T, agent string) string {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"install", agent})
	if err := root.Execute(); err != nil {
		t.Fatalf("glove install %s: %v\n%s", agent, err, out.String())
	}
	return out.String()
}

func uninstallViaCLI(t *testing.T, agent string) string {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"uninstall", agent})
	if err := root.Execute(); err != nil {
		t.Fatalf("glove uninstall %s: %v\n%s", agent, err, out.String())
	}
	return out.String()
}

// TestAC_0_5_2_kilo_hook_block_allow — AC0.5.2: the exact call the kilo
// shim makes (`glove hook kilo` with the shim envelope) blocks `rm -rf /`
// with exit 2 and allows `ls` with exit 0 — the automatable half of the
// live Kilo session acceptance.
func TestAC_0_5_2_kilo_hook_block_allow(t *testing.T) {
	hookTestEnv(t)
	t.Run("ac_0_5_2_block", func(t *testing.T) {
		in := `{"tool":"bash","args":{"command":"rm -rf /"},"session_id":"s1","call_id":"c1"}`
		var out, errBuf bytes.Buffer
		code := RunHookWith(&out, &errBuf, "kilo", strings.NewReader(in), HookOptions{})
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stdout: %s)", code, out.String())
		}
		var got struct {
			Decision string   `json:"decision"`
			Reasons  []string `json:"reasons"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("stdout not shim JSON: %v\n%s", err, out.String())
		}
		if got.Decision != "block" {
			t.Errorf("decision = %q, want block", got.Decision)
		}
		if len(got.Reasons) == 0 {
			t.Error("block verdict must carry the rule reasons (the shim surfaces them to the model)")
		}
	})
	t.Run("ac_0_5_2_allow", func(t *testing.T) {
		in := `{"tool":"bash","args":{"command":"ls"},"session_id":"s1","call_id":"c2"}`
		var out bytes.Buffer
		code := RunHook(&out, io.Discard, "kilo", strings.NewReader(in))
		if code != 0 {
			t.Fatalf("exit = %d, want 0 (stdout: %s)", code, out.String())
		}
		var got struct {
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("stdout not shim JSON: %v\n%s", err, out.String())
		}
		if got.Decision != "allow" {
			t.Errorf("decision = %q, want allow", got.Decision)
		}
	})
	t.Run("ac_0_5_2_review_code", func(t *testing.T) {
		in := `{"tool":"bash","args":{"command":"sudo ls"},"session_id":"s1","call_id":"c3"}`
		var out bytes.Buffer
		code := RunHook(&out, io.Discard, "kilo", strings.NewReader(in))
		if code != 0 {
			t.Fatalf("exit = %d, want 0 (review)", code)
		}
		var got struct {
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("stdout not shim JSON: %v", err)
		}
		if got.Decision != "review" {
			t.Errorf("decision = %q, want review (the shim turns it into the structured GLOVE_REVIEW throw)", got.Decision)
		}
	})
}

// TestAC_0_5_2_kilo_install_verify_flow — AC0.5.2 other half: the
// documented install flow for kilo — `glove install kilo` through the real
// CLI — writes the shim, verifies the engine (the same allow/block test
// calls the docs tell the user to expect), and reports health. The
// clean-machine ≤ 2 min half is a live-session measurement, recorded in
// RELEASE.md §5.
func TestAC_0_5_2_kilo_install_verify_flow(t *testing.T) {
	home := matrixTestEnv(t)
	out := installViaCLI(t, "kilo")
	if !strings.Contains(out, "install verified") {
		t.Errorf("install output = %q, want the verification result", out)
	}
	shimPath := filepath.Join(home, ".config", "kilo", "plugin", "glove.ts")
	raw, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("shim not written to %s: %v", shimPath, err)
	}
	if !strings.Contains(string(raw), "tool.execute.before") {
		t.Errorf("shim does not register tool.execute.before:\n%s", raw)
	}
	a := adapter.NewKilo()
	if st := adapter.StatusOf(a); !st.HookInstalled || st.Problem != "" {
		t.Errorf("status after install = %+v, want installed and healthy", st)
	}
	// Re-install is a no-op (the docs promise idempotency).
	out2 := installViaCLI(t, "kilo")
	if !strings.Contains(out2, "already installed") {
		t.Errorf("second install output = %q, want the already-installed notice", out2)
	}
	uninstallViaCLI(t, "kilo")
	if _, err := os.Stat(shimPath); !os.IsNotExist(err) {
		t.Errorf("shim still present after uninstall: %v", err)
	}
}

// TestAC_0_5_1_hook_audit_agent_attribution — AC0.5.1's audit deliverable:
// every adapter's hook path attributes the record to its own agent name.
func TestAC_0_5_1_hook_audit_agent_attribution(t *testing.T) {
	_, dataDir := hookTestEnv(t)
	for _, agent := range adapter.Names() {
		in := fixtureStdin(t, agent)
		var out bytes.Buffer
		code := RunHook(&out, io.Discard, agent, strings.NewReader(in))
		if code != 0 {
			t.Fatalf("%s: hook exit = %d (stdout %s)", agent, code, out.String())
		}
	}
	recs, err := audit.Load(dataDir)
	if err != nil {
		t.Fatalf("load audit: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Agent] = true
	}
	for _, agent := range adapter.Names() {
		if !seen[agent] {
			t.Errorf("no audit record attributed to %q (records: %v)", agent, seen)
		}
	}
}

// TestAC_0_5_4_doctor_detects_corrupted_config — AC0.5.4: after installing
// every agent, a deliberately corrupted hook config is reported unhealthy
// and doctor exits non-zero; with healthy configs it passes.
func TestAC_0_5_4_doctor_detects_corrupted_config(t *testing.T) {
	home := matrixTestEnv(t)
	for _, agent := range adapter.Names() {
		installViaCLI(t, agent)
	}
	t.Run("ac_0_5_4_healthy_chain", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runDoctorStatus(&buf); err != nil {
			t.Fatalf("doctor on healthy installs: %v\n%s", err, buf.String())
		}
		if got := buf.String(); !strings.Contains(got, "hook entry installed ✓") {
			t.Errorf("doctor output missing installed marker:\n%s", got)
		}
	})
	t.Run("ac_0_5_4_corrupted_detected", func(t *testing.T) {
		corruptions := map[string]string{
			".claude/settings.json":            "{\"hooks\": {broken",
			".config/kilo/plugin/glove.ts":     "not a glove shim, just garbage",
			".codex/hooks.json":                "{truncated",
			".config/opencode/plugin/glove.ts": "someone else's plugin",
			".cursor/hooks.json":               "{\"version\": 1, \"hooks\": {broken",
			".gemini/settings.json":            "{not json at all",
		}
		for rel, content := range corruptions {
			path := filepath.Join(home, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir for %s: %v", rel, err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("corrupt %s: %v", rel, err)
			}
		}
		var buf bytes.Buffer
		err := runDoctorStatus(&buf)
		if err == nil {
			t.Fatalf("doctor must exit non-zero with corrupted configs, output:\n%s", buf.String())
		}
		out := buf.String()
		for _, agent := range []string{"claude-code", "kilo", "codex", "opencode", "cursor", "gemini"} {
			if !strings.Contains(out, agent) || !strings.Contains(out, "UNHEALTHY") {
				t.Errorf("doctor output must flag %s as UNHEALTHY:\n%s", agent, out)
			}
		}
	})
}

// TestAC_0_5_5_list_health_matrix — AC0.5.5: `glove list` prints every
// registered agent with detection and hook health; installed agents show
// green health. Detection is machine-dependent (which agent binaries are on
// PATH), so the assertions key on the rows' health states, not on counts of
// detected agents.
func TestAC_0_5_5_list_health_matrix(t *testing.T) {
	home := matrixTestEnv(t)
	var buf bytes.Buffer
	if err := runList(&buf); err != nil {
		t.Fatalf("list: %v", buf.String())
	}
	out := buf.String()
	for _, agent := range adapter.Names() {
		if !strings.Contains(out, agent) {
			t.Errorf("list output missing agent %q:\n%s", agent, out)
		}
	}
	// Nothing installed yet: every row must show an absent hook — either
	// "NOT installed" (agent detected) or "not detected" (binary/config
	// missing on this machine).
	buf.Reset()
	if err := runList(&buf); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "installed ✓") {
			t.Errorf("fresh env must have no green rows, got: %q", line)
		}
	}
	// Install two and exactly their rows turn green.
	installViaCLI(t, "claude-code")
	installViaCLI(t, "kilo")
	buf.Reset()
	if err := runList(&buf); err != nil {
		t.Fatalf("list after install: %v", err)
	}
	out = buf.String()
	if n := strings.Count(out, "hook entry installed ✓"); n != 2 {
		t.Errorf("installed ✓ count = %d, want 2:\n%s", n, out)
	}
	if !strings.Contains(out, filepath.Join(".claude", "settings.json")) ||
		!strings.Contains(out, filepath.Join(".config", "kilo", "plugin", "glove.ts")) {
		t.Errorf("green rows must name their config paths:\n%s", out)
	}
	_ = home
}

// TestAC_0_5_5_list_zed_audit_mode — AC0.5.5 other half: zed's row carries
// mode audit.
func TestAC_0_5_5_list_zed_audit_mode(t *testing.T) {
	matrixTestEnv(t)
	var buf bytes.Buffer
	if err := runList(&buf); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "zed ") {
			found = true
			if !strings.Contains(line, "audit") {
				t.Errorf("zed row = %q, want mode audit", line)
			}
		}
	}
	if !found {
		t.Errorf("no zed row in list output:\n%s", out)
	}
}

// TestAC_0_5_6_matrix_matches_readme — AC0.5.6: the README coverage matrix
// is machine-verified by `glove list --matrix --check` — the real README
// must match the generated table, and drift must fail.
func TestAC_0_5_6_matrix_matches_readme(t *testing.T) {
	t.Run("ac_0_5_6_readme_matches", func(t *testing.T) {
		// Point --check at the real README (repo layout: internal/cli → root).
		prev := readmePath
		readmePath = filepath.Join("..", "..", "README.md")
		t.Cleanup(func() { readmePath = prev })
		var buf bytes.Buffer
		if err := runMatrix(&buf, true); err != nil {
			t.Fatalf("README coverage matrix drift — regenerate the block with `glove list --matrix`:\n%s\nerror: %v", buf.String(), err)
		}
	})
	t.Run("ac_0_5_6_drift_fails", func(t *testing.T) {
		fixture := filepath.Join(t.TempDir(), "README.md")
		drifted := "# glove\n\n" + matrixBegin + "\n\n" + renderMatrix() + "\n\n" + matrixEnd + "\n\nstale tail\n"
		// Introduce drift: drop a row.
		drifted = strings.Replace(drifted, "| kilo ", "| kilox ", 1)
		if err := os.WriteFile(fixture, []byte(drifted), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		prev := readmePath
		readmePath = fixture
		t.Cleanup(func() { readmePath = prev })
		var buf bytes.Buffer
		err := runMatrix(&buf, true)
		if err == nil {
			t.Fatal("drifted matrix must fail --check")
		}
		if !strings.Contains(buf.String(), "drift") {
			t.Errorf("output = %q, want a drift report", buf.String())
		}
	})
	t.Run("ac_0_5_6_no_to_be_verified_rows", func(t *testing.T) {
		table := renderMatrix()
		if strings.Contains(strings.ToLower(table), "to be verified") || strings.Contains(strings.ToLower(table), "planned") {
			t.Errorf("coverage matrix contains unverifiable rows:\n%s", table)
		}
	})
}

// TestAC_0_5_6_install_verify_flow_all_agents — AC0.5.6 other half: every
// registered agent has a docs page and its documented install + verify flow
// works through the real CLI: install (diff + backup + engine verification)
// → status green → uninstall → status cleared.
func TestAC_0_5_6_install_verify_flow_all_agents(t *testing.T) {
	matrixTestEnv(t)
	for _, agent := range adapter.Names() {
		t.Run(agent, func(t *testing.T) {
			doc := filepath.Join("..", "..", "docs", "install", agent+".md")
			raw, err := os.ReadFile(doc)
			if err != nil {
				t.Fatalf("docs page missing for %s (%s): %v", agent, doc, err)
			}
			if len(raw) < 200 {
				t.Errorf("docs page %s is a stub (%d bytes)", doc, len(raw))
			}
			out := installViaCLI(t, agent)
			if !strings.Contains(out, "install verified") && agent != "zed" {
				t.Errorf("install %s output missing verification: %q", agent, out)
			}
			if agent == "zed" && !strings.Contains(out, "audit mode") {
				t.Errorf("zed install output = %q, want the audit-mode statement", out)
			}
			a, ok := adapter.For(agent)
			if !ok {
				t.Fatalf("For(%q) does not resolve", agent)
			}
			st := adapter.StatusOf(a)
			if agent == "zed" {
				if st.Mode != adapter.ModeAudit {
					t.Errorf("zed status mode = %q, want audit", st.Mode)
				}
			} else if !st.HookInstalled || st.Problem != "" {
				t.Errorf("%s status after install = %+v, want installed and healthy", agent, st)
			}
			uninstallViaCLI(t, agent)
			if st := adapter.StatusOf(a); agent != "zed" && st.HookInstalled {
				t.Errorf("%s still shows the hook installed after uninstall: %+v", agent, st)
			}
		})
	}
}
