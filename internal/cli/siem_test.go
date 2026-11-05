package cli

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/audit"
)

// hookWithPolicy runs one in-process hook with a custom policy file.
func hookWithPolicy(t *testing.T, cfgDir string, policyToml, payload string) (int, string) {
	t.Helper()
	writeFile(t, filepath.Join(cfgDir, "policy.toml"), policyToml)
	var out, errBuf strings.Builder
	code := RunHookWith(&out, &errBuf, "claude-code", strings.NewReader(payload), HookOptions{})
	return code, out.String()
}

// TestAC_0_7_2_records_inherit_and_query_filter — AC0.7.2: audit records
// inherit the MITRE ATT&CK tags of the rules that fired, and
// `audit query --technique` filters on them (including user-tagged rules).
func TestAC_0_7_2_records_inherit_and_query_filter(t *testing.T) {
	cfgDir, dataDir := hookTestEnv(t)
	// rm -rf / fires rm_root (T1485); the user-tagged pattern fires T1565.
	policyToml := `
[general]
mode = "offline"
[rules]
hard_block = ["terraform\\s+destroy"]
[rules.techniques]
"terraform\\s+destroy" = "T1565"
`
	if code, _ := hookWithPolicy(t, cfgDir, policyToml, `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"},"session_id":"s1"}`); code != 2 {
		t.Fatalf("rm -rf / exit = %d, want 2", code)
	}
	if code, _ := hookWithPolicy(t, cfgDir, policyToml, `{"tool_name":"Bash","tool_input":{"command":"sudo terraform destroy"},"session_id":"s2"}`); code != 2 {
		t.Fatalf("sudo+user-hard-block exit = %d, want 2 (user hard_block blocks)", code)
	}
	if code, _ := hookWithPolicy(t, cfgDir, policyToml, `{"tool_name":"Bash","tool_input":{"command":"ls -la"},"session_id":"s3"}`); code != 0 {
		t.Fatalf("ls exit = %d, want 0", code)
	}

	recs := auditLines(t, dataDir)
	bySession := map[string]audit.Record{}
	for _, r := range recs {
		bySession[r.SessionID] = r
	}
	if got := bySession["s1"].Techniques; len(got) != 1 || got[0] != "T1485" {
		t.Fatalf("rm -rf / techniques = %v, want [T1485]", got)
	}
	// sudo fires T1548.003; terraform destroy fires the user tag T1565 —
	// both must appear, deduplicated.
	got := bySession["s2"].Techniques
	if len(got) != 2 || !((got[0] == "T1548.003" && got[1] == "T1565") || (got[0] == "T1565" && got[1] == "T1548.003")) {
		t.Fatalf("sudo+terraform techniques = %v, want T1548.003+T1565", got)
	}
	if bySession["s3"].Techniques != nil {
		t.Fatalf("ls techniques = %v, want none", bySession["s3"].Techniques)
	}

	// The query filter returns exactly the matching records.
	blocks, err := audit.QueryRecords(dataDir, audit.Query{Technique: "T1485"})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].SessionID != "s1" {
		t.Fatalf("--technique T1485 = %+v, want s1", blocks)
	}
	user, err := audit.QueryRecords(dataDir, audit.Query{Technique: "T1565"})
	if err != nil {
		t.Fatal(err)
	}
	if len(user) != 1 || user[0].SessionID != "s2" {
		t.Fatalf("--technique T1565 = %+v, want s2", user)
	}
}

// TestAC_0_7_5_sink_off_by_default — AC0.7.5: with no [audit.sink] config
// there are zero side effects — no sink file, no delivery events, hook
// behavior identical.
func TestAC_0_7_5_sink_off_by_default(t *testing.T) {
	cfgDir, dataDir := hookTestEnv(t)
	code, out := hookWithPolicy(t, cfgDir, "[general]\nmode = \"offline\"\n", `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
	if code != 2 || !strings.Contains(out, "deny") {
		t.Fatalf("decision changed with no sink configured: code=%d out=%q", code, out)
	}
	recs := auditLines(t, dataDir)
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1 (no sink artifacts)", len(recs))
	}
	for _, r := range recs {
		if r.Event == audit.EventSinkError || r.Event == audit.EventSinkDropped {
			t.Fatalf("sink event written with sink off: %+v", r)
		}
	}
	// Contrast: the same policy WITH a file sink writes the forward — proving
	// the off path is a real off, not a broken mechanism.
	forward := filepath.Join(t.TempDir(), "forward.jsonl")
	onPolicy := "[general]\nmode = \"offline\"\n\n[audit.sink]\ntype = \"file\"\ntarget = \"" + forward + "\"\n"
	cfgDir2, dataDir2 := hookTestEnv(t)
	code2, _ := hookWithPolicy(t, cfgDir2, onPolicy, `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
	if code2 != 2 {
		t.Fatalf("file-sink hook exit = %d, want 2", code2)
	}
	b, err := os.ReadFile(forward)
	if err != nil {
		t.Fatalf("file sink wrote nothing: %v", err)
	}
	if !strings.Contains(string(b), `"decision":"block"`) {
		t.Fatalf("forwarded record wrong: %q", b)
	}
	// The forwarded record must be the STAMPED one (ts, identity, chain) —
	// not the pre-write copy. Regression: the first draft forwarded the
	// caller's Record with zero ts and no schema version.
	var fwd audit.Record
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r audit.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("forward line not JSON: %v", err)
		}
		if r.Event == "" {
			fwd = r
		}
	}
	if fwd.SchemaVersion != audit.AuditSchemaVersion || fwd.Ts.IsZero() || fwd.MachineID == "" || fwd.RecordHash == "" {
		t.Fatalf("forwarded record not stamped: %+v", fwd)
	}
	recs2 := auditLines(t, dataDir2)
	if len(recs2) != 1 {
		t.Fatalf("local audit with sink on = %d records, want 1 (forwarding must not duplicate locally)", len(recs2))
	}
}

// TestAC_0_7_5_sink_failure_never_affects_verdict — AC0.7.5: a dead sink
// never changes the decision, the exit code, or the verdict latency; the
// failure is audited locally and the hook still returns promptly.
func TestAC_0_7_5_sink_failure_never_affects_verdict(t *testing.T) {
	t.Run("ac_0_7_5_dead_webhook", func(t *testing.T) {
		// Take a real port, then close it — dialing it refuses promptly.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("no tcp loopback: %v", err)
		}
		dead := ln.Addr().String()
		ln.Close()

		cfgDir, dataDir := hookTestEnv(t)
		policyToml := "[general]\nmode = \"offline\"\n\n[audit.sink]\ntype = \"webhook\"\ntarget = \"http://" + dead + "/collect\"\n"
		start := time.Now()
		code, out := hookWithPolicy(t, cfgDir, policyToml, `{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
		wall := time.Since(start)
		if code != 2 || !strings.Contains(out, "deny") {
			t.Fatalf("dead sink changed the decision: code=%d out=%q", code, out)
		}
		if wall > 5*time.Second {
			t.Fatalf("dead sink made the hook hang: %v", wall)
		}
		recs := auditLines(t, dataDir)
		var sawDecision, sawSinkError bool
		for _, r := range recs {
			switch {
			case r.Event == "":
				sawDecision = true
				if r.Decision != "block" {
					t.Fatalf("decision corrupted by sink failure: %+v", r)
				}
			case r.Event == audit.EventSinkError:
				sawSinkError = true
			}
		}
		if !sawDecision || !sawSinkError {
			t.Fatalf("want decision + sink_error event, got %+v", recs)
		}
	})

	t.Run("ac_0_7_5_slow_webhook_bounded", func(t *testing.T) {
		// A sink that stalls still can't hold the hook: the drain window
		// bounds the wait and the failure is audited.
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			<-release
			w.WriteHeader(200)
		}))
		defer srv.Close()
		defer close(release)

		cfgDir, dataDir := hookTestEnv(t)
		policyToml := "[general]\nmode = \"offline\"\n\n[audit.sink]\ntype = \"webhook\"\ntarget = \"" + srv.URL + "\"\ntimeout_ms = 200\n"
		start := time.Now()
		code, _ := hookWithPolicy(t, cfgDir, policyToml, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
		wall := time.Since(start)
		if code != 0 {
			t.Fatalf("slow sink changed the exit code: %d", code)
		}
		// 200ms drain window + slack — far under any §8 budget and proof the
		// wait is bounded, not unbounded.
		if wall > 3*time.Second {
			t.Fatalf("slow sink wait unbounded: %v", wall)
		}
		recs := auditLines(t, dataDir)
		sawSinkError := false
		for _, r := range recs {
			if r.Event == audit.EventSinkError {
				sawSinkError = true
			}
		}
		if !sawSinkError {
			t.Fatalf("stalled delivery not audited: %+v", recs)
		}
	})
}
