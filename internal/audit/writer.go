package audit

import (
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
type Writer struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	maxFiles int // total files kept, including the live audit.jsonl
	Now      func() time.Time
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
// the size cap.
func (w *Writer) Write(r Record) error {
	r.Ts = w.Now().UTC()
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	live := w.livePath()
	if info, err := os.Stat(live); err == nil && info.Size()+int64(len(line)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(live, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append audit record: %w", err)
	}
	return nil
}

// livePath is the current log file.
func (w *Writer) livePath() string {
	return filepath.Join(w.dir, "audit.jsonl")
}

// rotate shifts audit.jsonl → audit.jsonl.1 → … → .<maxFiles-1>, dropping
// the oldest.
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
