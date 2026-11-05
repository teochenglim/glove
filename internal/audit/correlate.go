package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Session escalation state (v0.8.0, AC0.8.1) is derived from the audit
// trail itself — keyed by session_id — so one-shot hook processes need no
// daemon (RELEASE/v0.8.0.md scope notes). The scan is deliberately bounded:
// the tail windows below cover the most recent records of the live file and
// the newest rotated file, which is where "repeated denies in this session"
// live. The cost is two bounded reads per *undecidable* call only — decided
// calls never pay it.

const (
	liveTailWindow    = 64 << 10 // 64 KB of audit.jsonl
	rotatedTailWindow = 16 << 10 // 16 KB of audit.jsonl.1
)

// CountSessionVerdicts counts the block/review decision records of one
// session in the bounded audit tail. Correlation is an additive tightening:
// an unreadable trail degrades to "no escalation" and never changes a
// decided verdict (failure-modes row).
func CountSessionVerdicts(dir, sessionID string) (blocks, reviews int, err error) {
	if dir == "" || sessionID == "" {
		return 0, 0, nil
	}
	count := func(path string, window int64) error {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		offset := int64(0)
		if info.Size() > window {
			offset = info.Size() - window
		}
		buf := make([]byte, info.Size()-offset)
		if _, err := f.ReadAt(buf, offset); err != nil {
			return err
		}
		sc := bufio.NewScanner(bytes.NewReader(buf))
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var probe struct {
				SessionID string `json:"session_id"`
				Decision  string `json:"decision"`
			}
			if json.Unmarshal(line, &probe) != nil {
				continue
			}
			if probe.SessionID != sessionID {
				continue
			}
			switch probe.Decision {
			case "block":
				blocks++
			case "review":
				reviews++
			}
		}
		return sc.Err()
	}
	if err := count(filepath.Join(dir, "audit.jsonl"), liveTailWindow); err != nil {
		return 0, 0, err
	}
	if err := count(filepath.Join(dir, rotatedName(1)), rotatedTailWindow); err != nil {
		return 0, 0, err
	}
	return blocks, reviews, nil
}
