package decision

import (
	"testing"

	"github.com/teochenglim/glove/pkg/contract"
)

// TestCombineWorstWins pins the §4.2 arithmetic Combine implements — the
// same arithmetic the v0.4.0 invariant test (engine_test.go) will exercise
// with a live Laya client.
func TestCombineWorstWins(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		allow := allowVerdict()
		review := allowVerdict()
		review.Decision = contract.DecisionReview
		review.Risk = contract.RiskHigh
		review.Reasons = append(review.Reasons, "sudo: privilege escalation")
		block := allowVerdict()
		block.Decision = contract.DecisionBlock
		block.Risk = contract.RiskCritical
		block.Reasons = append(block.Reasons, "rm_root: recursive delete on /")

		cases := []struct {
			name         string
			in           []contract.Verdict
			wantDecision contract.Decision
			wantRisk     contract.Risk
		}{
			{"single_allow", []contract.Verdict{allow}, contract.DecisionAllow, contract.RiskLow},
			{"review_beats_allow", []contract.Verdict{allow, review}, contract.DecisionReview, contract.RiskHigh},
			{"block_beats_all", []contract.Verdict{allow, review, block}, contract.DecisionBlock, contract.RiskCritical},
			{"block_beats_review", []contract.Verdict{review, block}, contract.DecisionBlock, contract.RiskCritical},
			{"inverted_order_same_result", []contract.Verdict{block, review, allow}, contract.DecisionBlock, contract.RiskCritical},
		}
		for _, tc := range cases {
			got := Combine(tc.in)
			if got.Decision != tc.wantDecision || got.Risk != tc.wantRisk {
				t.Errorf("%s: got %s/%s, want %s/%s", tc.name, got.Decision, got.Risk, tc.wantDecision, tc.wantRisk)
			}
		}
	})

	t.Run("reasons_merge", func(t *testing.T) {
		a := allowVerdict()
		a.Reasons = append(a.Reasons, "one")
		b := allowVerdict()
		b.Decision = contract.DecisionBlock
		b.Reasons = append(b.Reasons, "two")
		got := Combine([]contract.Verdict{a, b})
		if len(got.Reasons) != 2 || got.Reasons[0] != "one" || got.Reasons[1] != "two" {
			t.Errorf("reasons = %v, want [one two]", got.Reasons)
		}
	})

	t.Run("non_deterministic_contaminates", func(t *testing.T) {
		a := allowVerdict()
		b := allowVerdict()
		b.Deterministic = false
		if Combine([]contract.Verdict{a, b}).Deterministic {
			t.Error("combined verdict must lose determinism when any layer is non-deterministic")
		}
	})
}
