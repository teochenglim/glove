package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAC_0_7_3_contract_verdict_untouched — AC0.7.3: the 0.7.0 audit-only
// fields (hash chain, machine identity, techniques, schema version) live in
// the audit JSONL schema, NOT in the frozen §5.3 contract. The existing
// shape-freeze test (TestVerdictJSONShapeIsFrozen) pins the full field set;
// this pins the negative direction for the 0.7.0 additions.
func TestAC_0_7_3_contract_verdict_untouched(t *testing.T) {
	v := Verdict{
		Decision:      DecisionBlock,
		Risk:          RiskCritical,
		Confidence:    1,
		Deterministic: true,
		Reasons:       []string{"rm_root: recursive delete on /"},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, auditOnly := range []string{
		"schema_version", "prev_hash", "record_hash",
		"machine_id", "techniques",
	} {
		if strings.Contains(string(b), `"`+auditOnly+`"`) {
			t.Errorf("frozen contract leaked the audit-only field %q: %s", auditOnly, b)
		}
	}
}
