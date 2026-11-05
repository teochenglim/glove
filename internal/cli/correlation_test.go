package cli

import (
	"strings"
	"testing"

	"github.com/teochenglim/glove/internal/audit"
)

// runSession runs one hook payload under a session id and returns the exit
// code and stdout.
func runSession(t *testing.T, sessionID, command string) (int, string) {
	t.Helper()
	var out, errBuf strings.Builder
	payload := `{"tool_name":"Bash","tool_input":{"command":"` + command + `"},"session_id":"` + sessionID + `"}`
	code := RunHookWith(&out, &errBuf, "claude-code", strings.NewReader(payload), HookOptions{})
	return code, out.String()
}

// TestAC_0_8_1_escalation_tightens_undecidable — AC0.8.1: after the
// threshold of block/review decisions, a session's undecidable calls
// (nothing fired, bare allow) tighten to review; below the threshold they
// stay allow; session_threshold = -1 disables.
func TestAC_0_8_1_escalation_tightens_undecidable(t *testing.T) {
	cfgDir, dataDir := hookTestEnv(t)
	_ = cfgDir

	t.Run("ac_0_8_1_below_threshold_stays_allow", func(t *testing.T) {
		// One block is below the default threshold of 3: the undecidable
		// call stays allow.
		if code, _ := runSession(t, "fresh-session", "rm -rf /"); code != 2 {
			t.Fatalf("block exit = %d, want 2", code)
		}
		code, out := runSession(t, "fresh-session", "ls -la")
		if code != 0 || strings.Contains(out, `"ask"`) {
			t.Fatalf("undecidable call escalated below threshold: code=%d out=%q", code, out)
		}
	})

	t.Run("ac_0_8_1_at_threshold_tightens_to_review", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if code, _ := runSession(t, "esc-session", "rm -rf /"); code != 2 {
				t.Fatalf("setup block %d exit = %d, want 2", i, code)
			}
		}
		code, out := runSession(t, "esc-session", "ls -la")
		if code != 0 || !strings.Contains(out, `"ask"`) {
			t.Fatalf("undecidable call not tightened at threshold: code=%d out=%q", code, out)
		}
		// The verdict carries the correlation reason.
		if !strings.Contains(out, "correlation:") {
			t.Fatalf("no correlation reason in verdict: %q", out)
		}
		// The audit record holds the tightened decision.
		recs, err := audit.QueryRecords(dataDir, audit.Query{Agent: "claude-code"})
		if err != nil {
			t.Fatal(err)
		}
		var sawTightened bool
		for _, r := range recs {
			if r.SessionID == "esc-session" && r.Decision == "review" && r.Tool == "Bash" &&
				strings.Contains(strings.Join(r.Reasons, " "), "correlation:") {
				sawTightened = true
			}
		}
		if !sawTightened {
			t.Fatalf("tightened call not audited as review: %+v", recs)
		}
	})

	t.Run("ac_0_8_1_disabled_by_policy", func(t *testing.T) {
		writeFile(t, cfgDir+"/policy.toml", "[general]\nmode = \"offline\"\n\n[correlation]\nsession_threshold = -1\n")
		for i := 0; i < 4; i++ {
			if code, _ := runSession(t, "disabled-session", "rm -rf /"); code != 2 {
				t.Fatalf("setup block %d exit = %d", i, code)
			}
		}
		code, out := runSession(t, "disabled-session", "ls -la")
		if code != 0 || strings.Contains(out, `"ask"`) {
			t.Fatalf("correlation ran despite session_threshold = -1: code=%d out=%q", code, out)
		}
	})
}

// TestAC_0_8_1_correlation_never_loosens — AC0.8.1: correlation is an
// additive layer — deterministic verdicts in a hot session are untouched.
func TestAC_0_8_1_correlation_never_loosens(t *testing.T) {
	cfgDir, _ := hookTestEnv(t)
	_ = cfgDir
	// Heat the session past the threshold.
	for i := 0; i < 4; i++ {
		if code, _ := runSession(t, "hot-session", "rm -rf /"); code != 2 {
			t.Fatalf("setup block %d exit = %d", i, code)
		}
	}
	// A deterministic block stays block (and exit 2) — correlation may not
	// loosen it, and its reason must be absent.
	code, out := runSession(t, "hot-session", "rm -rf /")
	if code != 2 || !strings.Contains(out, "deny") {
		t.Fatalf("deterministic block loosened in a hot session: code=%d out=%q", code, out)
	}
	if strings.Contains(out, "correlation:") {
		t.Fatalf("correlation reason attached to a decided call: %q", out)
	}
	// A deterministic review (sudo fires force_confirm) stays review — not
	// downgraded to allow by correlation's presence, not double-annotated.
	code, out = runSession(t, "hot-session", "sudo ls")
	if code != 0 || !strings.Contains(out, `"ask"`) {
		t.Fatalf("deterministic review changed: code=%d out=%q", code, out)
	}
	if strings.Contains(out, "correlation:") {
		t.Fatalf("correlation reason attached to a decided review: %q", out)
	}
}
