package rules

import "testing"

// TestAC_0_7_2_rules_carry_technique_tags — AC0.7.2: builtin rules carry
// MITRE ATT&CK tags, user policy patterns can be tagged via [rules.techniques],
// and untagged rules contribute none.
func TestAC_0_7_2_rules_carry_technique_tags(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("builtin: %v", err)
	}
	want := map[string]string{
		"rm_root":                      "T1485",
		"sudo":                         "T1548.003",
		"chmod_777":                    "T1222.002",
		"curl_pipe_bash":               "T1059.004",
		"protected_path(**/.ssh/id_*)": "T1552.004",
		"protected_path(**/.env)":      "T1552.001",
	}
	for id, tag := range want {
		if got := builtin.TechniqueIndex[id]; got != tag {
			t.Errorf("technique[%s] = %q, want %q", id, got, tag)
		}
	}
	// Every builtin rule is tagged — a rule without a technique is a gap.
	for _, r := range builtin.HardBlock {
		if builtin.TechniqueIndex[r.ID] == "" {
			t.Errorf("hard_block %s has no technique tag", r.ID)
		}
	}
	for _, r := range builtin.ForceConfirm {
		if builtin.TechniqueIndex[r.ID] == "" {
			t.Errorf("force_confirm %s has no technique tag", r.ID)
		}
	}
	for _, r := range builtin.Composition {
		if builtin.TechniqueIndex[r.ID] == "" {
			t.Errorf("composition %s has no technique tag", r.ID)
		}
	}
	for _, r := range builtin.Protected {
		if builtin.TechniqueIndex[r.RuleID()] == "" {
			t.Errorf("protected_path %s has no technique tag", r.Pattern)
		}
	}

	t.Run("ac_0_7_2_user_pattern_technique", func(t *testing.T) {
		base, err := LoadBuiltin()
		if err != nil {
			t.Fatal(err)
		}
		rs, err := Compose(base, Extra{
			HardBlock: []string{"terraform\\s+destroy"},
			Protected: []string{"~/secrets/**"},
			Techniques: map[string]string{
				"terraform\\s+destroy": "T1565",
				"~/secrets/**":         "T1552.001",
			},
		})
		if err != nil {
			t.Fatalf("compose: %v", err)
		}
		if got := rs.TechniqueIndex["policy_hard_0"]; got != "T1565" {
			t.Errorf("user hard rule technique = %q, want T1565", got)
		}
		if got := rs.TechniqueIndex["protected_path(~/secrets/**)"]; got != "T1552.001" {
			t.Errorf("user protected path technique = %q, want T1552.001", got)
		}
	})

	t.Run("ac_0_7_2_untagged_user_rule_contributes_nothing", func(t *testing.T) {
		base, err := LoadBuiltin()
		if err != nil {
			t.Fatal(err)
		}
		rs, err := Compose(base, Extra{HardBlock: []string{"zebra\\s+pattern"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := rs.TechniqueIndex["policy_hard_0"]; got != "" {
			t.Errorf("untagged user rule has technique %q, want empty", got)
		}
	})
}
