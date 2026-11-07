package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/audit"
)

// TestAC_0_10_2_hook_block_allow_audit — AC0.10.2: the goose hook path
// enforces end-to-end — block rides the decision JSON and exit 2, allow
// rides {"decision":"allow"} and exit 0, and every invocation writes one
// audit record attributed to agent "goose" with the ATT&CK tags inherited.
func TestAC_0_10_2_hook_block_allow_audit(t *testing.T) {
	_, dataDir := hookTestEnv(t)

	t.Run("ac_0_10_2_block_decision_and_exit_2", func(t *testing.T) {
		var out, errBuf strings.Builder
		code := RunHookWith(&out, &errBuf, "goose",
			strings.NewReader(`{"event":"PreToolUse","session_id":"g1","tool_name":"shell","tool_input":{"command":"rm -rf /"},"working_dir":"/tmp"}`),
			HookOptions{})
		if code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		var got struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &got); err != nil {
			t.Fatalf("stdout not goose decision JSON: %v\n%s", err, out.String())
		}
		if got.Decision != "block" || !strings.Contains(got.Reason, "rm_root") {
			t.Fatalf("block decision = %+v", got)
		}
		if !strings.Contains(errBuf.String(), "rm_root") {
			t.Fatalf("stderr must carry the reason (goose exit-2 path): %q", errBuf.String())
		}
	})

	t.Run("ac_0_10_2_allow_decision_and_exit_0", func(t *testing.T) {
		var out, errBuf strings.Builder
		code := RunHookWith(&out, &errBuf, "goose",
			strings.NewReader(`{"event":"PreToolUse","session_id":"g1","tool_name":"shell","tool_input":{"command":"ls -la"},"working_dir":"/tmp"}`),
			HookOptions{})
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if strings.TrimSpace(out.String()) != `{"decision":"allow"}` {
			t.Fatalf("allow stdout = %q", out.String())
		}
	})

	t.Run("ac_0_10_2_audit_attribution_and_tags", func(t *testing.T) {
		recs, err := audit.QueryRecords(dataDir, audit.Query{Agent: "goose"})
		if err != nil {
			t.Fatal(err)
		}
		var sawBlock, sawAllow bool
		for _, r := range recs {
			if r.Tool == "shell" && r.Decision == "block" {
				sawBlock = true
				if len(r.Techniques) == 0 || r.Techniques[0] != "T1485" {
					t.Fatalf("block record techniques = %v, want T1485", r.Techniques)
				}
			}
			if r.Tool == "shell" && r.Decision == "allow" {
				sawAllow = true
			}
		}
		if !sawBlock || !sawAllow {
			t.Fatalf("audit records missing (block=%v allow=%v): %+v", sawBlock, sawAllow, recs)
		}
	})
}

// TestAC_0_10_2_install_fresh_home_under_two_minutes — AC0.10.2: the
// documented goose flow runs through the real binary in a fresh HOME —
// install, hooks.json verified on disk, a real subprocess hook call, then
// uninstall — under the two-minute budget.
func TestAC_0_10_2_install_fresh_home_under_two_minutes(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skips the binary build")
	}
	bin := buildGlove(t) // before the HOME override (0.6.1 lesson)
	home := matrixTestEnv(t)

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(),
			"HOME="+home,
			"USERPROFILE="+home,
			"GLOVE_CONFIG_DIR="+filepath.Join(home, "cfg"),
			"GLOVE_DATA_DIR="+filepath.Join(home, "data"),
		)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("glove %v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	if err := os.MkdirAll(filepath.Join(home, "cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "cfg", "policy.toml"), "[general]\nmode = \"offline\"\n")

	start := time.Now()
	installOut := run("install", "goose")
	if !strings.Contains(installOut, "installed") {
		t.Fatalf("install output = %q", installOut)
	}
	manifest, err := os.ReadFile(filepath.Join(home, ".agents", "plugins", "glove", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("manifest not written under the fresh HOME: %v", err)
	}
	if !strings.Contains(string(manifest), `"on_failure": "block"`) {
		t.Fatalf("manifest lost the fail-closed posture:\n%s", manifest)
	}
	doctor := run("doctor")
	if strings.Contains(doctor, "goose") && strings.Contains(doctor, "UNHEALTHY") {
		t.Fatalf("doctor reports goose unhealthy:\n%s", doctor)
	}
	// A real subprocess hook call: block decision + exit 2.
	hook := exec.Command(bin, "hook", "goose")
	hook.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"GLOVE_CONFIG_DIR="+filepath.Join(home, "cfg"),
		"GLOVE_DATA_DIR="+filepath.Join(home, "data"),
	)
	hook.Stdin = strings.NewReader(`{"event":"PreToolUse","session_id":"fresh","tool_name":"shell","tool_input":{"command":"rm -rf /"}}`)
	var hout bytes.Buffer
	hook.Stdout, hook.Stderr = &hout, &hout
	if err := hook.Run(); err == nil {
		t.Fatalf("blocked call exited 0:\n%s", hout.String())
	} else if !strings.Contains(hout.String(), `"decision":"block"`) {
		t.Fatalf("no block decision:\n%s", hout.String())
	}
	run("uninstall", "goose")
	if _, err := os.Stat(filepath.Join(home, ".agents", "plugins", "glove", "hooks", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest survived uninstall: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Minute {
		t.Fatalf("fresh-HOME goose flow took %v, budget 2 min", elapsed)
	}
	t.Logf("fresh-HOME goose flow (install, doctor, subprocess hook, uninstall): %v", elapsed)
}

// TestAC_0_10_6_docs_flow_complete — AC0.10.6: docs/install/goose.md
// carries the four documented sections with the ≤ 5 min box and the vendor
// caveats.
func TestAC_0_10_6_docs_flow_complete(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "install", "goose.md"))
	if err != nil {
		t.Fatalf("docs/install/goose.md missing: %v", err)
	}
	doc := string(raw)
	for _, want := range []string{
		"glove install goose", "glove doctor", "glove uninstall goose",
		"review", "deny", "on_failure", "Time: ~5 minutes",
		"fail-open by default", "SubagentStart",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/install/goose.md missing %q", want)
		}
	}
}

// TestAC_0_10_6_matrix_row_enforced — AC0.10.6: the README coverage matrix
// carries goose as enforced and `glove list --matrix --check` passes.
func TestAC_0_10_6_matrix_row_enforced(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "| goose | block / review (as deny — goose hooks are allow/block only)") {
		t.Fatal("README matrix is missing the goose enforced row")
	}
	// The machine check itself (renders the table from matrixRows and
	// compares against README between the markers). Point --check at the
	// real README (repo layout: internal/cli → root), the 0.5.6 pattern.
	prev := readmePath
	readmePath = filepath.Join("..", "..", "README.md")
	t.Cleanup(func() { readmePath = prev })
	cmd := newListCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--matrix", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list --matrix --check failed: %v\n%s", err, out.String())
	}
}
