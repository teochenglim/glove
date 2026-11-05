package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// VerifyReport is the outcome of `glove audit verify` (AC0.7.1).
type VerifyReport struct {
	Records  int     // total non-empty lines scanned
	Verified int     // schema-2 records whose hash and linkage check out
	Legacy   int     // schema-1 (pre-0.7.0) records — no chain fields, skipped
	Breaks   []Break // empty when the chain verified clean

	prev string // record_hash of the last verified record; "" = no anchor yet
}

// Break names the first broken record: where, which, and how the chain
// fails. Kinds: "corrupt-line" (unparseable — edit or truncation), "edited"
// (record_hash mismatch), "chain-gap" (prev_hash does not link — a record
// was deleted).
type Break struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	AuditID string `json:"audit_id,omitempty"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail,omitempty"`
}

// Ok reports whether the chain verified clean.
func (r VerifyReport) Ok() bool { return len(r.Breaks) == 0 }

// Verify walks the whole store in chain order (rotated .N → … → .1 → live)
// and re-computes every record_hash plus the prev_hash linkage, stopping at
// the first break (AC0.7.1 names the first broken record).
//
// Anchor rule: the first chainable (schema-2) record's prev_hash is accepted
// as-is — it may point into a rotated file that retention already dropped,
// or be empty at genesis. Middle deletions produce a chain-gap and are
// detected; a wholly-removed tail leaves no gap to detect (that class needs
// an external anchor and is out of scope — docs/siem.md). Legacy (schema-1)
// records carry no chain fields and are skipped without affecting the chain:
// on an upgrade the first v0.7.0 record chains from an empty prev_hash.
func Verify(dir string) (VerifyReport, error) {
	rep := VerifyReport{}
	var paths []string
	for i := maxRotated(dir); i >= 1; i-- {
		paths = append(paths, filepath.Join(dir, rotatedName(i)))
	}
	paths = append(paths, filepath.Join(dir, "audit.jsonl"))

	for _, p := range paths {
		if err := verifyFile(p, &rep); err != nil && !os.IsNotExist(err) {
			return rep, err
		}
		if len(rep.Breaks) > 0 {
			break
		}
	}
	return rep, nil
}

func verifyFile(path string, rep *VerifyReport) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := []byte(trimSpace(sc.Text()))
		if len(line) == 0 {
			continue
		}
		rep.Records++
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			rep.Breaks = append(rep.Breaks, Break{
				File: filepath.Base(path), Line: lineNo, Kind: "corrupt-line",
				Detail: "unparseable record — edited or truncated",
			})
			return nil
		}
		if r.SchemaVersion < AuditSchemaVersion {
			rep.Legacy++
			continue
		}
		// Recompute the record hash exactly as the writer built it.
		stored := r.RecordHash
		r.RecordHash = ""
		h, err := chainHash(r)
		if err != nil {
			return fmt.Errorf("rehash %s: %w", path, err)
		}
		if h != stored {
			rep.Breaks = append(rep.Breaks, Break{
				File: filepath.Base(path), Line: lineNo, AuditID: r.AuditID, Kind: "edited",
				Detail: "record_hash mismatch — record content was modified",
			})
			return nil
		}
		if rep.prev != "" && r.PrevHash != rep.prev {
			rep.Breaks = append(rep.Breaks, Break{
				File: filepath.Base(path), Line: lineNo, AuditID: r.AuditID, Kind: "chain-gap",
				Detail: "prev_hash does not link — a record was deleted",
			})
			return nil
		}
		rep.Verified++
		rep.prev = stored
	}
	return sc.Err()
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
