package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer appends Records as JSON lines to <dir>/audit.jsonl and rotates at
// maxBytes into numbered backups, keeping maxFiles files total (PRD §5.4:
// 10 MB × 5). Safe for concurrent use; the clock is injected so rotation
// behaviour is testable without sleeping.
//
// Since v0.7.0 every written record is chained (AC0.7.1): record_hash =
// sha256 over the record with RecordHash zeroed, and prev_hash carries the
// previous record's hash — across rotation boundaries too (the chain runs
// over the whole store: rotated .N → … → .1 → live). The mutex serialises
// writes, so the chain is exact under concurrency.
type Writer struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	maxFiles int // total files kept, including the live audit.jsonl
	Now      func() time.Time

	chainInit bool
	lastHash  string // record_hash of the last written record (this process)
}

// NewWriter returns a writer for dir with the PRD §5.4 defaults.
func NewWriter(dir string) *Writer {
	return &Writer{
		dir:      dir,
		maxBytes: 10 << 20, // 10 MB
		maxFiles: 5,
		Now:      time.Now,
	}
}

// Write appends one record, rotating first when the live file would exceed
// the size cap. It returns the stamped record — ts, schema version, machine
// identity and chain fields are the Writer's to set, and callers that
// forward the record onward (the sink, ADR 4) must forward this stamped
// copy, not their input.
func (w *Writer) Write(r Record) (Record, error) {
	r.Ts = w.Now().UTC()
	r.SchemaVersion = AuditSchemaVersion
	if r.Host == "" {
		id := Identity()
		r.Host, r.User, r.MachineID = id.Host, id.User, id.MachineID
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return Record{}, fmt.Errorf("create audit dir: %w", err)
	}
	if !w.chainInit {
		w.lastHash = tailRecordHash(w.dir)
		w.chainInit = true
	}
	r.PrevHash = w.lastHash
	r.RecordHash = ""
	hash, err := chainHash(r)
	if err != nil {
		return Record{}, fmt.Errorf("hash audit record: %w", err)
	}
	r.RecordHash = hash

	line, err := json.Marshal(r)
	if err != nil {
		return Record{}, fmt.Errorf("encode audit record: %w", err)
	}
	line = append(line, '\n')

	live := w.livePath()
	if info, err := os.Stat(live); err == nil && info.Size()+int64(len(line)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return Record{}, err
		}
	}
	f, err := os.OpenFile(live, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Record{}, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return Record{}, fmt.Errorf("append audit record: %w", err)
	}
	w.lastHash = r.RecordHash
	return r, nil
}

// chainHash computes record_hash over the canonical JSON of r with
// RecordHash zeroed (struct field order is fixed, so the bytes are stable
// for verify to reproduce). r.PrevHash must already be set.
func chainHash(r Record) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// tailRecordHash finds the record_hash of the last record already on disk —
// the live file's tail, or (after a rotation with nothing written since) the
// newest rotated file's tail. Empty when the store is fresh.
func tailRecordHash(dir string) string {
	paths := []string{filepath.Join(dir, "audit.jsonl")}
	if n := maxRotated(dir); n >= 1 {
		paths = append(paths, filepath.Join(dir, rotatedName(1)))
	}
	for _, p := range paths {
		if h, ok := lastLineField(p, "record_hash"); ok {
			return h
		}
	}
	return ""
}

// lastLineField extracts a JSON field from the last non-empty line of a
// file, reading at most the trailing 64 KB.
func lastLineField(path, field string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	const window = 64 << 10
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return "", false
	}
	offset := int64(0)
	size := info.Size()
	if size > window {
		offset = size - window
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return "", false
	}
	lines := splitLines(buf)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 {
			continue
		}
		var probe map[string]any
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		if v, ok := probe[field].(string); ok && v != "" {
			return v, true
		}
		return "", false // last parseable line has no such field (legacy)
	}
	return "", false
}

func splitLines(buf []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range buf {
		if b == '\n' {
			out = append(out, buf[start:i])
			start = i + 1
		}
	}
	if start < len(buf) {
		out = append(out, buf[start:])
	}
	return out
}

// livePath is the current log file.
func (w *Writer) livePath() string {
	return filepath.Join(w.dir, "audit.jsonl")
}

// rotate shifts audit.jsonl → audit.jsonl.1 → … → .<maxFiles-1>, dropping
// the oldest. The chain is unaffected: the first record of the new live file
// carries prev_hash of the last record of the rotated file (AC0.7.1).
func (w *Writer) rotate() error {
	oldest := filepath.Join(w.dir, rotatedName(w.maxFiles-1))
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("drop oldest audit file: %w", err)
	}
	for i := w.maxFiles - 2; i >= 1; i-- {
		from := filepath.Join(w.dir, rotatedName(i))
		to := filepath.Join(w.dir, rotatedName(i+1))
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("shift audit rotation: %w", err)
		}
	}
	if err := os.Rename(w.livePath(), filepath.Join(w.dir, rotatedName(1))); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate audit log: %w", err)
	}
	return nil
}

func rotatedName(i int) string {
	return fmt.Sprintf("audit.jsonl.%d", i)
}

// Dir exposes the audit directory (CLI output).
func (w *Writer) Dir() string { return w.dir }
