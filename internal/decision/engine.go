package decision

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/rules"
	"github.com/teochenglim/glove/pkg/contract"
)

// Engine evaluates tool calls through the layer pipeline (PRD §4.1):
// Layer 0 deterministic rules per chain segment, Layer 1 composition rules
// on pipe pairs and shell invocations (v0.2.0). Layer 2 (Laya) joins in
// v0.4.0 and must route through Combine like everything else.
type Engine struct {
	rules *rules.RuleSet
}

// New wraps a compiled rule set.
func New(rs *rules.RuleSet) *Engine {
	return &Engine{rules: rs}
}

// NewBuiltin loads the embedded builtin.yaml rule set (PRD Appendix B).
func NewBuiltin() (*Engine, error) {
	rs, err := rules.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	return New(rs), nil
}

// SegmentVerdict is the per-segment outcome behind a decision (CLI --explain).
type SegmentVerdict struct {
	Text     string
	Via      string
	Compound string
	Decision contract.Decision
	Risk     contract.Risk
	Reasons  []string
	// Pair is the joined pipe text when this segment closes a pipe into a
	// shell (composition risk is only visible as a pair, PRD §7 0.2.0).
	Pair     string
	PairRisk contract.Risk
	PairWhy  []string
}

// Evaluate runs the pipeline over one tool call and stamps the audit ID.
// LatencyMs is measured by the caller (CLI boundary — the only place a wall
// clock is allowed to read).
func (e *Engine) Evaluate(in adapter.HookInput) contract.Verdict {
	v, _ := e.EvaluateDetailed(in)
	v.AuditID = uuid.NewString()
	return v
}

// EvaluateDetailed additionally returns the per-segment verdicts.
func (e *Engine) EvaluateDetailed(in adapter.HookInput) (contract.Verdict, []SegmentVerdict) {
	if shellTools[in.ToolName] {
		if cmd, ok := commandOf(in.ToolInput); ok && strings.TrimSpace(cmd) != "" {
			return e.evaluateChain(cmd)
		}
		return allowVerdict(), nil
	}
	// Non-shell tools (Edit, Write, MCP, ...): Layer 0 only claims protected
	// paths — any string field referencing one blocks the call.
	return e.evaluateOtherTool(in.ToolInput), nil
}

var shellTools = map[string]bool{
	"Bash": true,
	"sh":   true,
	"bash": true,
	"zsh":  true,
}

// evaluateChain is Layer 0+1 for a shell command string: split into segments,
// evaluate each deterministically, evaluate composition on pipe pairs into
// shells, combine worst-part-wins (§4.2 — a review or block in one segment
// can never be downgraded by an allow in another).
func (e *Engine) evaluateChain(cmd string) (contract.Verdict, []SegmentVerdict) {
	segs := Split(cmd)
	var partials []contract.Verdict
	var details []SegmentVerdict
	lastMain := ""

	for _, s := range segs {
		var segPartials []contract.Verdict
		segPartials = append(segPartials, e.evaluateSegment(s.Text))
		// Composition rules also run on shell-invocation segments —
		// `sh -c "$(curl ...)"` is compound without any pipe.
		if !s.inner && isShellInvocation(s.Text) {
			segPartials = append(segPartials, e.evaluateComposition(s.Text))
		}
		if s.Compound == compoundUnbalanced {
			v := allowVerdict()
			v.Decision = contract.DecisionReview
			v.Risk = contract.RiskMedium
			v.Reasons = append(v.Reasons, "subshell: unbalanced command substitution — contents not visible to deterministic rules")
			segPartials = append(segPartials, v)
		}
		sv := Combine(segPartials)
		detail := SegmentVerdict{
			Text:     s.Text,
			Via:      s.Via,
			Compound: s.Compound,
			Decision: sv.Decision,
			Risk:     sv.Risk,
			Reasons:  sv.Reasons,
		}

		if s.Via == "|" && !s.inner && isShellInvocation(s.Text) && lastMain != "" {
			pair := lastMain + " | " + s.Text
			pv := e.evaluateComposition(pair)
			partials = append(partials, pv)
			if pv.Decision == contract.DecisionBlock {
				detail.Pair = pair
				detail.PairRisk = pv.Risk
				detail.PairWhy = pv.Reasons
			}
		}
		partials = append(partials, sv)
		details = append(details, detail)
		if !s.inner {
			lastMain = s.Text
		}
	}
	return Combine(partials), details
}

// evaluateSegment is Layer 0 for one segment: protected paths, hard blocks,
// force confirms — combined worst-part-wins so a segment carrying several
// findings reports all of them. A quote-stripped pass catches dangers hidden
// behind quoting (`bash -c 'rm -rf /'`).
func (e *Engine) evaluateSegment(cmd string) contract.Verdict {
	var partials []contract.Verdict

	if hits := e.rules.PathHits(cmd); len(hits) > 0 {
		v := allowVerdict()
		v.Decision = contract.DecisionBlock
		v.Risk = contract.RiskHigh
		for _, h := range hits {
			v.Reasons = append(v.Reasons, fmtReason("protected_path("+h.Pattern+")", h.Reason))
		}
		partials = append(partials, v)
	}
	if r := e.rules.FirstHardHit(cmd); r != nil {
		partials = append(partials, blockCritical(r.ID, r.Reason))
	}
	if r := e.rules.FirstForceHit(cmd); r != nil {
		partials = append(partials, reviewHigh(r.ID, r.Reason))
	}
	if stripped := stripQuotes(cmd); stripped != cmd {
		if r := e.rules.FirstHardHit(stripped); r != nil {
			partials = append(partials, blockCritical(r.ID+" (quoted)", r.Reason))
		}
	}
	if len(partials) == 0 {
		return allowVerdict()
	}
	return Combine(partials)
}

// evaluateComposition applies the composition rules to a chain fragment —
// a joined pipe pair ("curl http://x | sh") or an invocation segment. All
// hits are reported.
func (e *Engine) evaluateComposition(text string) contract.Verdict {
	hits := e.rules.CompositionHits(text)
	if len(hits) == 0 {
		return allowVerdict()
	}
	v := allowVerdict()
	v.Decision = contract.DecisionBlock
	v.Risk = contract.RiskCritical
	for _, h := range hits {
		v.Reasons = append(v.Reasons, fmtReason(h.ID, h.Reason))
	}
	return v
}

// evaluateOtherTool scans every string value of a non-shell tool input
// against the protected-path globs. Reading OR writing a credential is a
// block; everything else about non-shell tools is Layer 2 territory
// (v0.4.0).
func (e *Engine) evaluateOtherTool(raw json.RawMessage) contract.Verdict {
	if len(raw) == 0 {
		return allowVerdict()
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Unparseable non-shell input is not something Layer 0 can claim —
		// it is left for later layers. Layer 0 stays honest.
		return allowVerdict()
	}
	joined := strings.Join(rules.StringValues(doc), " ")
	if hits := e.rules.PathHits(joined); len(hits) > 0 {
		v := allowVerdict()
		v.Decision = contract.DecisionBlock
		v.Risk = contract.RiskHigh
		for _, h := range hits {
			v.Reasons = append(v.Reasons, fmtReason("protected_path("+h.Pattern+")", h.Reason))
		}
		return v
	}
	return allowVerdict()
}

func blockCritical(id, reason string) contract.Verdict {
	v := allowVerdict()
	v.Decision = contract.DecisionBlock
	v.Risk = contract.RiskCritical
	v.Reasons = append(v.Reasons, fmtReason(id, reason))
	return v
}

func reviewHigh(id, reason string) contract.Verdict {
	v := allowVerdict()
	v.Decision = contract.DecisionReview
	v.Risk = contract.RiskHigh
	v.Reasons = append(v.Reasons, fmtReason(id, reason))
	return v
}

// stripQuotes removes single and double quote characters — used only as an
// additional matching pass, never as the displayed text.
func stripQuotes(s string) string {
	if !strings.ContainsAny(s, "'\"") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\'' || r == '"' {
			return -1
		}
		return r
	}, s)
}

func commandOf(raw json.RawMessage) (string, bool) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Command == "" {
		return "", false
	}
	return in.Command, true
}

func allowVerdict() contract.Verdict {
	return contract.Verdict{
		Decision:       contract.DecisionAllow,
		Risk:           contract.RiskLow,
		Confidence:     1,
		Deterministic:  true,
		ModelAvailable: false,
		Reasons:        []string{},
	}
}

func fmtReason(id, reason string) string {
	return id + ": " + reason
}
