package decision

import (
	"testing"

	"github.com/teochenglim/glove/pkg/contract"
)

// TestAC_0_4_3_layer2_never_downgrades — AC0.4.3, the PRD §4.2 invariant
// with the mock §10.1 specified back at 0.1.0: a Layer 0 `block` fed through
// a mocked Laya `allow` must end `block` — and a Laya `review` cannot erase
// it either. Combine is the enforcement point.
func TestAC_0_4_3_layer2_never_downgrades(t *testing.T) {
	e := testEngine(t)
	base := e.Evaluate(bashInput("rm -rf /"))
	if base.Decision != contract.DecisionBlock {
		t.Fatalf("deterministic verdict = %s, want block", base.Decision)
	}

	t.Run("ac_0_4_3_mocked_laya_allow_loses", func(t *testing.T) {
		l2 := contract.Verdict{
			Decision:       contract.DecisionAllow,
			Risk:           contract.RiskLow,
			Confidence:     0.99,
			Deterministic:  false,
			ModelAvailable: true,
		}
		got := Combine([]contract.Verdict{base, l2})
		if got.Decision != contract.DecisionBlock {
			t.Fatalf("final = %s, want block (the guard only ever tightens)", got.Decision)
		}
		if !got.ModelAvailable {
			t.Error("model_available must be true when Laya answered")
		}
		if len(got.Questions) != 0 {
			t.Errorf("questions = %v, want none from a bare mock", got.Questions)
		}
	})

	t.Run("ac_0_4_3_mocked_laya_review_loses", func(t *testing.T) {
		l2 := contract.Verdict{
			Decision:       contract.DecisionReview,
			Risk:           contract.RiskMedium,
			Confidence:     0.9,
			Deterministic:  false,
			ModelAvailable: true,
			Reasons:        []string{"laya: calibrated uncertainty"},
		}
		got := Combine([]contract.Verdict{base, l2})
		if got.Decision != contract.DecisionBlock {
			t.Fatalf("final = %s, want block over a Laya review", got.Decision)
		}
		if !contains(got.Reasons, "laya: calibrated uncertainty") {
			t.Errorf("reasons = %v, want the Laya reason preserved", got.Reasons)
		}
	})
}
