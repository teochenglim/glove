package contract

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestVerdictJSONShapeIsFrozen guards the §5.3 contract: the JSON key set of
// Verdict must not drift. Changing it requires a documented schema-version
// bump (PRD §10.4) — this test is the tripwire.
func TestVerdictJSONShapeIsFrozen(t *testing.T) {
	v := Verdict{
		Decision:       DecisionBlock,
		Risk:           RiskCritical,
		Confidence:     0.99,
		Reasons:        []string{"recursive delete on /"},
		Deterministic:  true,
		ModelAvailable: false,
		Questions:      []QA{{Question: "destructive", Answer: true, Confidence: 0.97}},
		AuditID:        "6f6c44e2-9c58-4f0a-9a52-4b0f6e0f3d21",
		LatencyMs:      3,
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal Verdict: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal Verdict: %v", err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	want := []string{
		"audit_id",
		"confidence",
		"decision",
		"deterministic",
		"latency_ms",
		"model_available",
		"questions",
		"reasons",
		"risk",
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("Verdict JSON keys drifted from the frozen §5.3 schema:\n got: %v\nwant: %v", keys, want)
	}
}

// TestVerdictOmittedOptionalKey checks the one omitempty field: with no
// Layer 2 questions, "questions" must be absent, not null.
func TestVerdictOmittedOptionalKey(t *testing.T) {
	raw, err := json.Marshal(Verdict{Decision: DecisionAllow, Risk: RiskLow})
	if err != nil {
		t.Fatalf("marshal Verdict: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal Verdict: %v", err)
	}
	if _, present := got["questions"]; present {
		t.Fatalf("empty Questions must omit the key, got: %s", raw)
	}
}

// TestDecisionAndRiskEnumValues pins the frozen enum strings; adapters and
// the audit schema match on these exact byte sequences (PRD §5.1, §5.4).
func TestDecisionAndRiskEnumValues(t *testing.T) {
	decisions := map[Decision]string{
		DecisionAllow:  "allow",
		DecisionReview: "review",
		DecisionBlock:  "block",
	}
	risks := map[Risk]string{
		RiskLow:      "low",
		RiskMedium:   "medium",
		RiskHigh:     "high",
		RiskCritical: "critical",
	}
	for d, want := range decisions {
		if string(d) != want {
			t.Errorf("Decision %q != frozen value %q", d, want)
		}
	}
	for r, want := range risks {
		if string(r) != want {
			t.Errorf("Risk %q != frozen value %q", r, want)
		}
	}
	if len(decisions) != 3 || len(risks) != 4 {
		t.Fatalf("enum cardinality drifted: %d decisions, %d risks", len(decisions), len(risks))
	}
}
