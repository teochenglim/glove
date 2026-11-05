package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/audit"
)

// TestAC_0_8_2_stats_top_tables — AC0.8.2: the aggregation counts
// decisions, agents, rules (from reason prefixes, "(quoted)" collapsed) and
// techniques; event records are excluded; output is deterministic.
func TestAC_0_8_2_stats_top_tables(t *testing.T) {
	_, dataDir := hookTestEnv(t)
	w := audit.NewWriter(dataDir)
	mk := func(id, agent, decision string, reasons []string, techniques []string) audit.Record {
		return audit.Record{
			AuditID: id, Agent: agent, Tool: "Bash", Decision: decision,
			Risk: "high", Reasons: reasons, Techniques: techniques,
			GloveVersion: "test",
		}
	}
	recs := []audit.Record{
		mk("1", "claude-code", "block", []string{"rm_root: recursive delete on /"}, []string{"T1485"}),
		mk("2", "claude-code", "review", []string{"sudo: privilege escalation"}, []string{"T1548.003"}),
		mk("3", "kilo", "allow", nil, nil),
		mk("4", "claude-code", "block", []string{"rm_root (quoted): recursive delete on /"}, []string{"T1485"}),
		mk("5", "kilo", "review", []string{"protected_path(**/.env): .env file"}, []string{"T1552.001"}),
		{AuditID: "ev", Event: audit.EventPolicyReload, Reasons: []string{"policy changed"}, GloveVersion: "test"},
	}
	for _, r := range recs {
		if _, err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := audit.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	st := audit.ComputeStats(loaded)
	if st.Records != 5 {
		t.Fatalf("records = %d, want 5 (event excluded)", st.Records)
	}
	if st.Decisions["block"] != 2 || st.Decisions["review"] != 2 || st.Decisions["allow"] != 1 {
		t.Fatalf("decision counts = %+v", st.Decisions)
	}
	if st.Agents[0].Name != "claude-code" || st.Agents[0].N != 3 {
		t.Fatalf("top agent = %+v, want claude-code 3", st.Agents[0])
	}
	// rm_root: raw + quoted collapse onto one rule, 2 hits; ties are
	// name-ordered (rm_root before sudo at equal... sudo has 1, rm_root 2).
	if st.Rules[0].Name != "rm_root" || st.Rules[0].N != 2 {
		t.Fatalf("top rule = %+v, want rm_root 2", st.Rules[0])
	}
	if st.Rules[1].Name != "protected_path(**/.env)" || st.Rules[2].Name != "sudo" {
		t.Fatalf("rule order = %+v", st.Rules)
	}
	if len(st.Techniques) != 3 || st.Techniques[0].Name != "T1485" || st.Techniques[0].N != 2 {
		t.Fatalf("techniques = %+v", st.Techniques)
	}

	// The CLI rendering is deterministic and honors --top.
	cmd := newAuditStatsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--top", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"records: 5", "top agents: claude-code 3", "top rules: rm_root 2", "top techniques: T1485 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("stats output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sudo") && strings.Contains(got, "top rules: rm_root 2, sudo") {
		t.Errorf("--top 1 leaked a second rule entry:\n%s", got)
	}
}

// TestAC_0_8_2_stats_10k_under_200ms — AC0.8.2: correct on the 10k fixture
// within the 200ms budget. Strict budget asserted in `make stress` (CI
// stays deterministic per the 0.6.2 stress split).
func TestAC_0_8_2_stats_10k_under_200ms(t *testing.T) {
	requireStress(t)
	_, dataDir := hookTestEnv(t)
	w := audit.NewWriter(dataDir)
	now := time.Now()
	for i := 0; i < 10_000; i++ {
		r := audit.Record{
			AuditID: fmt.Sprintf("bench-%d", i), Agent: "claude-code", Tool: "Bash",
			Decision:     []string{"allow", "review", "block"}[i%3],
			Risk:         "high",
			Reasons:      []string{"sudo: privilege escalation"},
			Techniques:   []string{"T1548.003"},
			Ts:           now.Add(-time.Duration(i) * time.Second),
			GloveVersion: "test",
		}
		if _, err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	recs, err := audit.QueryRecords(dataDir, audit.Query{})
	if err != nil {
		t.Fatal(err)
	}
	st := audit.ComputeStats(recs)
	elapsed := time.Since(start)
	if st.Records != 10_000 {
		t.Fatalf("records = %d, want 10000", st.Records)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("stats over 10k records took %v, budget 200ms", elapsed)
	}
}
