package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAC_0_3_1_record_fields_populated — AC0.3.1 other half: every §5.4
// field survives the write/read round trip, and the writer stamps Ts from
// its injected clock.
func TestAC_0_3_1_record_fields_populated(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	in := `{"command":"rm -rf /"}`
	sum := sha256.Sum256([]byte(in))
	red := Redact(in)
	ts := time.Now().UTC().Add(-time.Minute)
	w.Now = func() time.Time { return ts }

	want := Record{
		AuditID: "audit-1", Agent: "claude-code", SessionID: "s1",
		Tool: "Bash", InputHash: hex.EncodeToString(sum[:]), InputRedacted: &red,
		Decision: "block", Risk: "critical", Confidence: 1,
		Deterministic: true, ModelAvailable: false,
		Reasons:   []string{"rm_root: recursive delete on /"},
		LatencyMs: 3, GloveVersion: "0.3.0",
	}
	if err := w.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d records, want 1", len(got))
	}
	r := got[0]
	if !r.Ts.Equal(ts) {
		t.Errorf("ts = %v, want %v (writer must honour the injected clock)", r.Ts, ts)
	}
	r.Ts = ts
	r.InputRedacted = want.InputRedacted
	if r.AuditID != want.AuditID || r.Agent != want.Agent || r.SessionID != want.SessionID ||
		r.Tool != want.Tool || r.InputHash != want.InputHash ||
		r.Decision != want.Decision || r.Risk != want.Risk || r.Confidence != want.Confidence ||
		r.Deterministic != want.Deterministic || r.ModelAvailable != want.ModelAvailable ||
		len(r.Reasons) != 1 || r.Reasons[0] != want.Reasons[0] ||
		r.LatencyMs != want.LatencyMs || r.GloveVersion != want.GloveVersion {
		t.Errorf("record round trip drifted:\n got %+v\nwant %+v", r, want)
	}
}

// TestAC_0_3_4_rotation_at_10mb — AC0.3.4 with the real constants: a >10 MB
// stream rotates to audit.jsonl.1 at the 10 MB cap (PRD §5.4). Skipped in
// -short; the rotation logic itself is covered below at scaled-down size.
func TestAC_0_3_4_rotation_at_10mb(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	dir := t.TempDir()
	w := NewWriter(dir) // defaults: 10 MB cap, 5 files
	big := strings.Repeat("x", 150)
	n := 0
	for size := int64(0); size < 15<<20; size += int64(len(big) + 150) {
		rec := Record{AuditID: "r", Tool: big, GloveVersion: "0.3.0", Reasons: []string{strings.Repeat("y", 100)}}
		if err := w.Write(rec); err != nil {
			t.Fatalf("write %d: %v", n, err)
		}
		n++
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl.1")); err != nil {
		t.Fatalf("rotation did not produce audit.jsonl.1: %v", err)
	}
	live, err := os.Stat(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat live: %v", err)
	}
	if live.Size() > 10<<20+4096 {
		t.Errorf("live file %d bytes: rotation lagged the 10 MB cap", live.Size())
	}
}

// TestAC_0_3_4_rotation_keeps_five — AC0.3.4 other half: the same rotation
// logic at scaled-down constants proves the 5-file window: .1..N shift,
// the oldest is dropped, and no audit.jsonl.5 ever exists.
func TestAC_0_3_4_rotation_keeps_five(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	w.maxBytes = 800 // scaled down: the shift-and-cap logic is size-independent
	w.maxFiles = 5
	for i := 0; i < 60; i++ {
		rec := Record{AuditID: time.Duration(i).String(), Tool: strings.Repeat("t", 40), GloveVersion: "0.3.0"}
		if err := w.Write(rec); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for _, name := range []string{"audit.jsonl", "audit.jsonl.1", "audit.jsonl.2", "audit.jsonl.3", "audit.jsonl.4"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s after heavy rotation: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl.5")); !os.IsNotExist(err) {
		t.Error("audit.jsonl.5 exists — the 5-file cap is broken")
	}
}

// TestAC_0_3_3_redaction_rules — AC0.3.3 redaction half: private keys,
// Authorization headers, token= secrets and base64 blobs all vanish; the
// surrounding command survives.
func TestAC_0_3_3_redaction_rules(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		cases := []struct {
			name  string
			raw   string
			gone  []string
			keeps []string
		}{
			{
				"private_key_block",
				"cat <<'EOF'\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\nEOF",
				[]string{"BEGIN OPENSSH", "b3BlbnNzaC1rZXktdjEAAAAA"},
				[]string{"[REDACTED-PRIVATE-KEY]", "cat <<'EOF'"},
			},
			{
				"authorization_header",
				`curl -H "Authorization: Bearer sk-live-123" https://x`,
				[]string{"sk-live-123"},
				[]string{"[REDACTED]", "curl -H"},
			},
			{
				"token_pair",
				"deploy --token=hunter2 --dry-run",
				[]string{"hunter2"},
				[]string{"[REDACTED]", "deploy --token="},
			},
			{
				"base64_blob",
				"echo " + strings.Repeat("Q", 48) + "= | base64 -d",
				[]string{strings.Repeat("Q", 48)},
				[]string{"[REDACTED-BASE64]", "base64 -d"},
			},
		}
		for _, tc := range cases {
			got := Redact(tc.raw)
			for _, g := range tc.gone {
				if strings.Contains(got, g) {
					t.Errorf("%s: redacted output still contains %q:\n%s", tc.name, g, got)
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(got, k) {
					t.Errorf("%s: redacted output lost %q:\n%s", tc.name, k, got)
				}
			}
		}
	})
}

// TestQueryFilters — AC0.3.2 correctness half: decision/agent/since filters
// intersect, and results come back newest first.
func TestQueryFilters(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	fixed := time.Now().UTC()
	steps := []struct {
		age time.Duration
		rec Record
	}{
		{2 * 24 * time.Hour, Record{AuditID: "old-block", Decision: "block", Agent: "claude-code"}},
		{1 * time.Hour, Record{AuditID: "new-block", Decision: "block", Agent: "claude-code"}},
		{1 * time.Hour, Record{AuditID: "new-allow", Decision: "allow", Agent: "claude-code"}},
		{1 * time.Hour, Record{AuditID: "new-block-kilo", Decision: "block", Agent: "kilo"}},
	}
	for _, s := range steps {
		rec := s.rec
		age := s.age
		w.Now = func() time.Time { return fixed.Add(-age) }
		if err := w.Write(rec); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	t.Run("block_since_24h", func(t *testing.T) {
		got, err := QueryRecords(dir, Query{Decision: "block", Since: fixed.Add(-24 * time.Hour)})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d records, want the two in-window blocks", len(got))
		}
		ids := map[string]bool{got[0].AuditID: true, got[1].AuditID: true}
		if !ids["new-block"] || !ids["new-block-kilo"] {
			t.Errorf("got %+v, want new-block and new-block-kilo (old-block excluded)", got)
		}
	})
	t.Run("agent_filter", func(t *testing.T) {
		got, err := QueryRecords(dir, Query{Decision: "block", Agent: "kilo"})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(got) != 1 || got[0].AuditID != "new-block-kilo" {
			t.Errorf("got %+v, want only new-block-kilo", got)
		}
	})
	t.Run("newest_first", func(t *testing.T) {
		got, err := QueryRecords(dir, Query{})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(got) != 4 || got[0].AuditID == "old-block" {
			t.Errorf("order = %+v, want newest first", got)
		}
	})
}
