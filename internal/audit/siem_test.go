package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// siemFixture builds a 100-record mixed fixture (AC0.7.4): all three
// decisions, all four risks, pipes and equals in the reasons to exercise the
// escaping, multilingual reason text.
func siemFixture(n int) []Record {
	out := make([]Record, 0, n)
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	decisions := []string{"allow", "review", "block"}
	risks := []string{"low", "medium", "high", "critical"}
	for i := 0; i < n; i++ {
		r := Record{
			Ts:        base.Add(time.Duration(i) * time.Minute),
			AuditID:   "fix-" + itoa(i),
			Agent:     []string{"claude-code", "kilo", "gemini"}[i%3],
			SessionID: "sess-" + itoa(i%7),
			Tool:      []string{"Bash", "Write", "mcp__fs__read"}[i%3],
			InputHash: "hash" + itoa(i),
			Decision:  decisions[i%3],
			Risk:      risks[i%4],
			Reasons: []string{
				"curl_pipe_bash: download piped into a shell",
				"载荷包含 | 竖线 = 等号",
			},
			Techniques:    []string{"T1059.004", "T1552.001"},
			Confidence:    0.75,
			Deterministic: i%2 == 0,
			LatencyMs:     int64(3 + i%9),
			GloveVersion:  "v0.7.0",
			Host:          "bench-1", User: "ci", MachineID: "0123456789abcdef",
		}
		out = append(out, r)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// TestAC_0_7_4_cef_export_fixture — AC0.7.4: 100-record fixture renders as
// CEF lines with 8 header fields, escaped pipes/equals, and the decision
// extensions present.
func TestAC_0_7_4_cef_export_fixture(t *testing.T) {
	for i, r := range siemFixture(100) {
		line := CEF(r)
		if !strings.HasPrefix(line, "CEF:0|glove|glove|") {
			t.Fatalf("record %d: bad CEF prefix: %q", i, line)
		}
		// Split the header (first 7 pipes) — extension values may contain
		// escaped pipes only, so a raw pipe inside the extension would break
		// the 7-field prefix invariant only if unescaped.
		parts := strings.SplitN(line, "|", 8)
		if len(parts) != 8 {
			t.Fatalf("record %d: header fields = %d, want 8: %q", i, len(parts), line)
		}
		// Mapped severities: allow=2 (+2 critical=4), review=5 (+2=7),
		// block=8 (+2=10).
		mapped := map[string]bool{"2": true, "4": true, "5": true, "7": true, "8": true, "10": true}
		if !mapped[parts[6]] {
			t.Fatalf("record %d: severity %q out of the mapped set", i, parts[6])
		}
		for _, want := range []string{"fileHash=", "cs2Label=techniques cs2=", "cs3Label=machine_id cs3=", "msg="} {
			if !strings.Contains(line, want) {
				t.Fatalf("record %d: missing extension %s in %q", i, want, line)
			}
		}
		if r.Decision == "block" && r.Risk == "critical" && parts[6] != "10" {
			t.Fatalf("record %d: block+critical severity = %s, want 10", i, parts[6])
		}
		// The multilingual reason must survive verbatim (only = is escaped
		// inside extension values).
		if !strings.Contains(line, `载荷包含 | 竖线 \= 等号`) {
			t.Fatalf("record %d: multilingual reason not escaped/present: %q", i, line)
		}
	}
}

// TestAC_0_7_4_ecs_export_fixture — AC0.7.4: 100-record fixture renders as
// ECS documents carrying the required ECS fields.
func TestAC_0_7_4_ecs_export_fixture(t *testing.T) {
	for i, r := range siemFixture(100) {
		b, err := ECS(r)
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("record %d: not JSON: %v", i, err)
		}
		for _, field := range []string{"@timestamp", "ecs", "event", "agent", "user", "glove"} {
			if _, ok := doc[field]; !ok {
				t.Fatalf("record %d: missing ECS field %q", i, field)
			}
		}
		ev := doc["event"].(map[string]any)
		for _, field := range []string{"kind", "dataset", "id", "action", "outcome"} {
			if v, _ := ev[field].(string); v == "" {
				t.Fatalf("record %d: event.%s empty", i, field)
			}
		}
		wantOutcome := map[string]string{"allow": "success", "block": "failure", "review": "unknown"}[r.Decision]
		if ev["outcome"] != wantOutcome {
			t.Fatalf("record %d: outcome %v, want %s", i, ev["outcome"], wantOutcome)
		}
		glove := doc["glove"].(map[string]any)
		if glove["decision"] != r.Decision || glove["machine_id"] != r.MachineID {
			t.Fatalf("record %d: glove block lost the raw decision/machine_id", i)
		}
		if ts, _ := doc["@timestamp"].(string); !strings.HasPrefix(ts, "2026-11-06T") {
			t.Fatalf("record %d: @timestamp = %q", i, ts)
		}
	}
}
