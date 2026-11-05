package audit

import (
	"sort"
	"strings"
)

// Stats is the `glove audit stats` aggregation (v0.8.0, AC0.8.2) — the
// local "SIEM-lite" view: top rules, techniques, agents, and decision
// counts over a window. Pure aggregation over loaded records; the CLI owns
// the --since window and the --top cut.
type Stats struct {
	Records    int
	Decisions  map[string]int // allow/review/block counts (events excluded)
	Agents     []Count
	Rules      []Count
	Techniques []Count
}

// Count is one named bucket with its hit count.
type Count struct {
	Name string
	N    int
}

// ComputeStats aggregates records (oldest→newest or newest→oldest — order
// does not matter for the counts). Rule ids come from the reason prefix
// ("rm_root: recursive delete on /" → rm_root); the "(quoted)" suffix the
// engine appends collapses back onto the base rule; a rule firing twice in
// one record (the 0.2-era raw+segment double pass) counts once — stats
// counts records-per-rule, not reasons-per-record. Engine reasons that are
// not rule hits (subshell notes, correlation escalations) are skipped.
// Event records (policy_reload, sink_error, …) are excluded entirely.
func ComputeStats(recs []Record) Stats {
	st := Stats{Decisions: map[string]int{}}
	rules := map[string]int{}
	agents := map[string]int{}
	techniques := map[string]int{}
	for _, r := range recs {
		if r.Event != "" {
			continue
		}
		st.Records++
		st.Decisions[r.Decision]++
		agents[r.Agent]++
		seen := map[string]bool{}
		for _, id := range ruleIDsFromReasons(r.Reasons) {
			if seen[id] {
				continue
			}
			seen[id] = true
			rules[id]++
		}
		for _, t := range r.Techniques {
			techniques[t]++
		}
	}
	st.Agents = topN(agents, 0)
	st.Rules = topN(rules, 0)
	st.Techniques = topN(techniques, 0)
	return st
}

// nonRulePrefixes are engine reason prefixes that are not rule hits — the
// correlation escalation (v0.8.0) and the subshell note (v0.2.0).
var nonRulePrefixes = map[string]bool{
	"subshell":    true,
	"correlation": true,
}

// ruleIDsFromReasons extracts the fired rule ids from the reason prefixes
// the engine writes ("id: reason"). Reasons without a recognizable prefix
// (layer-2 text, correlation notes) are skipped — the table stays honest.
func ruleIDsFromReasons(reasons []string) []string {
	var out []string
	for _, r := range reasons {
		id, ok := ruleIDOf(r)
		if ok {
			out = append(out, id)
		}
	}
	return out
}

func ruleIDOf(reason string) (string, bool) {
	idx := strings.Index(reason, ": ")
	if idx <= 0 {
		return "", false
	}
	id := reason[:idx]
	// The engine's quoted-pass marker collapses onto the base rule.
	id = strings.TrimSuffix(id, " (quoted)")
	if id == "" || strings.Contains(id, " ") || nonRulePrefixes[id] {
		return "", false
	}
	return id, true
}

// topN sorts the map into a descending slice; n <= 0 keeps everything (the
// CLI applies its --top cut on the rendered side).
func topN(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{Name: k, N: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Name < out[j].Name // stable, deterministic output
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
