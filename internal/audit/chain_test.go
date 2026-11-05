package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rec builds a decision record for the chain tests.
func rec(id, decision string) Record {
	return Record{
		AuditID: id, Agent: "claude-code", Tool: "Bash",
		InputHash: "deadbeef", Decision: decision, Risk: "high",
		Reasons:      []string{"rm_root: recursive delete on /"},
		GloveVersion: "test",
	}
}

// TestAC_0_7_1_verify_detects_edits_deletes_truncation — AC0.7.1: verify
// accepts a clean log and names the first broken record when one is edited,
// deleted, or the file is truncated/corrupted.
func TestAC_0_7_1_verify_detects_edits_deletes_truncation(t *testing.T) {
	t.Run("ac_0_7_1_clean_log_verifies_green", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		for i := 0; i < 10; i++ {
			if _, err := w.Write(rec(string(rune('a'+i)), "allow")); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !rep.Ok() {
			t.Fatalf("clean log reported breaks: %+v", rep.Breaks)
		}
		if rep.Verified != 10 || rep.Records != 10 {
			t.Fatalf("verified=%d records=%d, want 10/10", rep.Verified, rep.Records)
		}
	})

	t.Run("ac_0_7_1_edit_named", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		for i := 0; i < 5; i++ {
			if _, err := w.Write(rec(string(rune('a'+i)), "allow")); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		// Edit record c's decision in place.
		b, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		edited := strings.Replace(lines[2], `"decision":"allow"`, `"decision":"review"`, 1)
		if edited == lines[2] {
			t.Fatal("edit did not land")
		}
		lines[2] = edited
		if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if rep.Ok() || len(rep.Breaks) != 1 {
			t.Fatalf("want exactly one break, got %+v", rep.Breaks)
		}
		br := rep.Breaks[0]
		if br.Kind != "edited" || br.Line != 3 {
			t.Fatalf("want edited at line 3, got %s line %d", br.Kind, br.Line)
		}
		if br.AuditID != "c" {
			t.Fatalf("break must name the record (audit_id=c), got %q", br.AuditID)
		}
	})

	t.Run("ac_0_7_1_delete_makes_gap", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		for i := 0; i < 5; i++ {
			if _, err := w.Write(rec(string(rune('a'+i)), "allow")); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		b, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("fixture lines = %d, want 5", len(lines))
		}
		kept := append(append([]string{}, lines[:2]...), lines[3:]...) // drop record c
		if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if rep.Ok() {
			t.Fatal("deletion went undetected")
		}
		if rep.Breaks[0].Kind != "chain-gap" {
			t.Fatalf("want chain-gap, got %+v", rep.Breaks[0])
		}
	})

	t.Run("ac_0_7_1_truncation_named", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		for i := 0; i < 3; i++ {
			if _, err := w.Write(rec(string(rune('a'+i)), "allow")); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		// Corrupt the last line in place (crash-style truncation).
		b, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		lines[2] = lines[2][:len(lines[2])/2]
		if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if rep.Ok() {
			t.Fatal("truncation went undetected")
		}
		if rep.Breaks[0].Kind != "corrupt-line" || rep.Breaks[0].Line != 3 {
			t.Fatalf("want corrupt-line at 3, got %+v", rep.Breaks[0])
		}
	})

	t.Run("ac_0_7_1_legacy_records_skipped", func(t *testing.T) {
		dir := t.TempDir()
		// A pre-0.7.0 (schema-1) line: no chain fields, no schema_version.
		legacy := `{"ts":"2026-01-01T00:00:00Z","audit_id":"legacy-1","agent":"claude-code","tool":"Bash","input_hash":"aa","decision":"allow","risk":"low","confidence":1,"deterministic":true,"model_available":false,"reasons":[],"latency_ms":1,"glove_version":"0.6.4"}`
		if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(legacy+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		w := NewWriter(dir)
		if _, err := w.Write(rec("first-v2", "allow")); err != nil {
			t.Fatal(err)
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !rep.Ok() || rep.Legacy != 1 || rep.Verified != 1 {
			t.Fatalf("want ok with 1 legacy + 1 verified, got %+v", rep)
		}
	})
}

// TestAC_0_7_1_chain_concurrent_and_rotation — AC0.7.1: the chain holds
// under concurrent writers and across rotation boundaries.
func TestAC_0_7_1_chain_concurrent_and_rotation(t *testing.T) {
	t.Run("ac_0_7_1_concurrent_writers", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < 25; i++ {
					if _, err := w.Write(rec(string(rune('A'+g))+"-"+string(rune('a'+i)), "allow")); err != nil {
						t.Errorf("write: %v", err)
						return
					}
				}
			}(g)
		}
		wg.Wait()
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !rep.Ok() || rep.Verified != 200 {
			t.Fatalf("chain broken under concurrency: %+v", rep)
		}
	})

	t.Run("ac_0_7_1_chain_survives_rotation", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir)
		w.maxBytes = 1024 // force rotation early
		w.maxFiles = 3
		for i := 0; i < 40; i++ {
			if _, err := w.Write(rec(string(rune('a'+i%26))+string(rune('a'+i/26)), "allow")); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "audit.jsonl.1")); err != nil {
			t.Fatalf("fixture produced no rotation: %v", err)
		}
		rep, err := Verify(dir)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !rep.Ok() {
			t.Fatalf("chain broken across rotation: %+v", rep.Breaks)
		}
		// The first live record must link to the last rotated record.
		rot, err := os.ReadFile(filepath.Join(dir, "audit.jsonl.1"))
		if err != nil {
			t.Fatal(err)
		}
		live, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		liveLines := strings.Split(strings.TrimRight(string(live), "\n"), "\n")
		var first Record
		if err := json.Unmarshal([]byte(liveLines[0]), &first); err != nil {
			t.Fatal(err)
		}
		rotLines := strings.Split(strings.TrimRight(string(rot), "\n"), "\n")
		var lastRot Record
		if err := json.Unmarshal([]byte(rotLines[len(rotLines)-1]), &lastRot); err != nil {
			t.Fatal(err)
		}
		if first.PrevHash != lastRot.RecordHash {
			t.Fatal("live chain does not continue the rotated file's chain")
		}
	})
}

// TestAC_0_7_3_machine_identity_in_records — AC0.7.3: every record carries
// host/user/machine_id and the derived machine_id is stable.
func TestAC_0_7_3_machine_identity_in_records(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	for i := 0; i < 3; i++ {
		if _, err := w.Write(rec(string(rune('a'+i)), "allow")); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := Identity()
	if id.Host == "" || id.User == "" || len(id.MachineID) != 16 {
		t.Fatalf("identity incomplete: %+v", id)
	}
	for _, r := range recs {
		if r.Host != id.Host || r.User != id.User || r.MachineID != id.MachineID {
			t.Fatalf("record %s identity mismatch: %+v vs %+v", r.AuditID, r.Host+"/"+r.User+"/"+r.MachineID, id)
		}
		if r.SchemaVersion != AuditSchemaVersion {
			t.Fatalf("record %s schema_version = %d", r.AuditID, r.SchemaVersion)
		}
	}
	// machine_id is sha256(host+"/"+user), first 16 hex.
	want := machineIDForTest(id.Host, id.User)
	if id.MachineID != want {
		t.Fatalf("machine_id = %s, want %s", id.MachineID, want)
	}
}

func machineIDForTest(host, user string) string {
	// Mirror identity.go's derivation.
	sum := sha256.Sum256([]byte(host + "/" + user))
	return hex.EncodeToString(sum[:])[:16]
}
