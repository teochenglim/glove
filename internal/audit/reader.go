package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Query filters records (PRD §6 `glove audit query`). Zero-value fields are
// "no filter". Technique (v0.7.0) matches records whose MITRE ATT&CK tags
// include the given technique.
type Query struct {
	Decision  string
	Agent     string
	Technique string
	Since     time.Time
}

// Load reads every record from the audit directory, oldest first: the live
// audit.jsonl plus the rotated audit.jsonl.N files, rotated ones first.
func Load(dir string) ([]Record, error) {
	var paths []string
	for i := maxRotated(dir); i >= 1; i-- {
		paths = append(paths, filepath.Join(dir, rotatedName(i)))
	}
	paths = append(paths, filepath.Join(dir, "audit.jsonl"))

	var out []Record
	for _, p := range paths {
		recs, err := readFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

func readFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			// One corrupt line must not sink the whole query — skip and keep
			// reading (the log is append-only; partial lines are possible on
			// a crash).
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

// maxRotated finds the highest existing audit.jsonl.N.
func maxRotated(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "audit.jsonl.%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max
}

// QueryRecords returns the records matching q, newest first.
func QueryRecords(dir string, q Query) ([]Record, error) {
	recs, err := Load(dir)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, r := range recs {
		if q.Decision != "" && r.Decision != q.Decision {
			continue
		}
		if q.Agent != "" && r.Agent != q.Agent {
			continue
		}
		if q.Technique != "" && !containsTechnique(r, q.Technique) {
			continue
		}
		if !q.Since.IsZero() && r.Ts.Before(q.Since) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.After(out[j].Ts) })
	return out, nil
}

// Tail returns the last n records, oldest first.
func Tail(dir string, n int) ([]Record, error) {
	recs, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if n > 0 && len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	return recs, nil
}

func containsTechnique(r Record, technique string) bool {
	for _, t := range r.Techniques {
		if t == technique {
			return true
		}
	}
	return false
}
