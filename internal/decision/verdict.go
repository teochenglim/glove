// Package decision is Glove's three-layer pipeline (PRD §4.1). v0.1.0 ships
// Layer 0 (deterministic rules) only; Combine already implements the §4.2
// "only ever tightens" arithmetic, and every later layer (chain splitting
// v0.2.0, Laya v0.4.0) must route its verdicts through it.
package decision

import "github.com/teochenglim/glove/pkg/contract"

func decisionRank(d contract.Decision) int {
	switch d {
	case contract.DecisionBlock:
		return 2
	case contract.DecisionReview:
		return 1
	case contract.DecisionAllow:
		return 0
	default:
		return -1
	}
}

func riskRank(r contract.Risk) int {
	switch r {
	case contract.RiskCritical:
		return 3
	case contract.RiskHigh:
		return 2
	case contract.RiskMedium:
		return 1
	case contract.RiskLow:
		return 0
	default:
		return -1
	}
}

// Combine merges partial verdicts worst-part-wins (PRD §4.2):
//
//	final = block if any layer returned block
//	        else review if any layer returned review
//	        else allow
//
// This function is the invariant's enforcement point: a Layer 2 "allow" is
// combined with — never substituted for — a Layer 0 "block", so the block
// survives by construction. ModelAvailable is an OR: "did any layer consult
// the model" — the deterministic layers never do, so a Layer 2 presence is
// what makes it true.
func Combine(vs []contract.Verdict) contract.Verdict {
	out := contract.Verdict{
		Decision:       contract.DecisionAllow,
		Risk:           contract.RiskLow,
		Confidence:     1,
		Deterministic:  true,
		ModelAvailable: false,
		Reasons:        []string{},
	}
	for _, v := range vs {
		if decisionRank(v.Decision) > decisionRank(out.Decision) {
			out.Decision = v.Decision
		}
		if riskRank(v.Risk) > riskRank(out.Risk) {
			out.Risk = v.Risk
		}
		if v.Confidence < out.Confidence {
			out.Confidence = v.Confidence
		}
		if !v.Deterministic {
			out.Deterministic = false
		}
		if v.ModelAvailable {
			out.ModelAvailable = true
		}
		out.Reasons = append(out.Reasons, v.Reasons...)
		out.Questions = append(out.Questions, v.Questions...)
	}
	return out
}
