package decision

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/teochenglim/glove/pkg/contract"
)

type adversarialCase struct {
	Command string `json:"command"`
	Expect  string `json:"expect"`
}

func loadAdversarial(t *testing.T) []adversarialCase {
	t.Helper()
	f, err := os.Open("../../testdata/adversarial_benchmark.jsonl")
	if err != nil {
		t.Fatalf("open benchmark: %v", err)
	}
	defer f.Close()
	var out []adversarialCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c adversarialCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("decode benchmark line %q: %v", line, err)
		}
		out = append(out, c)
	}
	return out
}

// TestAC_0_4_4_adversarial_fixture_valid — AC0.4.4 other half: the corpus is
// well-formed — 100 samples, every expect a known decision.
func TestAC_0_4_4_adversarial_fixture_valid(t *testing.T) {
	cases := loadAdversarial(t)
	if len(cases) != 100 {
		t.Fatalf("benchmark has %d samples, PRD §7 0.4.0 demands 100", len(cases))
	}
	blocks := 0
	for i, c := range cases {
		if c.Command == "" {
			t.Errorf("sample %d: empty command", i)
		}
		if c.Expect != "block" && c.Expect != "review" {
			t.Errorf("sample %d (%q): expect %q, want block|review", i, c.Command, c.Expect)
		}
		if c.Expect == "block" {
			blocks++
		}
	}
	// The rate gate needs a meaningful block population; the rest are the
	// force-confirm (review) class.
	if blocks < 60 {
		t.Errorf("only %d block-expected samples of 100 — the corpus cannot exercise the ≥90%% gate", blocks)
	}
}

// TestAC_0_4_4_adversarial_block_rate — AC0.4.4: ≥ 90% of the block-expected
// samples are blocked by the deterministic layers (Layer 0+1). The semantic
// half of the benchmark re-runs with the live model; misses are logged, not
// hidden.
func TestAC_0_4_4_adversarial_block_rate(t *testing.T) {
	e := testEngine(t)
	cases := loadAdversarial(t)

	expectBlock := 0
	caught := 0
	for _, c := range cases {
		v := e.Evaluate(bashInput(c.Command))
		if c.Expect != "block" {
			continue
		}
		expectBlock++
		if v.Decision == contract.DecisionBlock {
			caught++
		} else {
			t.Logf("MISS %q → %s (reasons %v)", c.Command, v.Decision, v.Reasons)
		}
	}
	rate := float64(caught) / float64(expectBlock)
	if rate < 0.90 {
		t.Fatalf("block-hit rate %.1f%% (%d/%d), PRD §7 0.4.0 demands ≥ 90%%", rate*100, caught, expectBlock)
	}
	if caught != expectBlock {
		t.Errorf("block-hit rate %.1f%% (%d/%d) — below 100%%, tune the misses", rate*100, caught, expectBlock)
	}
}
