package decision

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/policy"
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

// NewFromPolicy composes the builtin rule set with the resolved policy's
// extensions (PRD §5.5, §7 0.3.0): every hook/check process starts from the
// current policy, so user edits are enforced on the next tool call with no
// daemon to restart.
func NewFromPolicy(p *policy.Policy) (*Engine, error) {
	base, err := rules.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	rs, err := rules.Compose(base, rules.Extra{
		HardBlock:    p.Rules.HardBlock,
		ForceConfirm: p.Rules.ForceConfirm,
		Protected:    p.Paths.Protected,
		Techniques:   p.Rules.Techniques,
	})
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

// RuleCounts exposes the composed rule counts (doctor status output):
// hard-block, force-confirm, composition, protected paths.
func (e *Engine) RuleCounts() (int, int, int, int) {
	return len(e.rules.HardBlock), len(e.rules.ForceConfirm), len(e.rules.Composition), len(e.rules.Protected)
}

// Evaluate runs the pipeline over one tool call and stamps the audit ID.
// LatencyMs is measured by the caller (CLI boundary — the only place a wall
// clock is allowed to read).
func (e *Engine) Evaluate(in adapter.HookInput) contract.Verdict {
	v, _, _ := e.EvaluateDetailed(in)
	v.AuditID = uuid.NewString()
	return v
}

// EvaluateDetailed additionally returns the per-segment verdicts and the
// ids of every deterministic rule that fired (v0.7.0: the hook maps them to
// MITRE ATT&CK technique tags for the audit record, AC0.7.2).
func (e *Engine) EvaluateDetailed(in adapter.HookInput) (contract.Verdict, []SegmentVerdict, []string) {
	if shellTools[in.ToolName] {
		if cmd, ok := commandOf(in.ToolInput); ok && strings.TrimSpace(cmd) != "" {
			return e.evaluateChain(cmd)
		}
		return allowVerdict(), nil, nil
	}

	// Non-shell tools (Edit, Write, MCP, ...): Layer 0 only claims protected
	// paths — any string field referencing one blocks the call.
	v, fired := e.evaluateOtherTool(in.ToolInput)
	return v, nil, fired
}

// TechniquesFor resolves fired rule ids to their MITRE ATT&CK tags,
// deduplicated, order-stable, empty tags dropped (AC0.7.2).
func (e *Engine) TechniquesFor(ruleIDs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ruleIDs {
		t := e.rules.TechniqueIndex[id]
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
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
func (e *Engine) evaluateChain(cmd string) (contract.Verdict, []SegmentVerdict, []string) {
	segs := Split(cmd)
	var partials []contract.Verdict
	var details []SegmentVerdict
	var fired []string
	lastMain := ""

	// Raw-command pass FIRST: the splitter shreds patterns that span
	// operators (the classic fork bomb `:(){ :|:& };:` becomes fragments),
	// so the whole command is also matched against the deterministic rules.
	// Worst-part-wins keeps this strictly additive.
	raw, ids := e.evaluateSegment(cmd)
	fired = append(fired, ids...)
	if raw.Decision != contract.DecisionAllow {
		partials = append(partials, raw)
	}

	for _, s := range segs {
		var segPartials []contract.Verdict
		seg, segIDs := e.evaluateSegment(s.Text)
		segPartials = append(segPartials, seg)
		fired = append(fired, segIDs...)
		// Composition rules also run on shell-invocation segments —
		// `sh -c "$(curl ...)"` is compound without any pipe.
		if !s.inner && isShellInvocation(s.Text) {
			comp, compIDs := e.evaluateComposition(s.Text)
			segPartials = append(segPartials, comp)
			fired = append(fired, compIDs...)
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
			pv, pairIDs := e.evaluateComposition(pair)
			partials = append(partials, pv)
			fired = append(fired, pairIDs...)
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
	return Combine(partials), details, fired
}

// evaluateSegment is Layer 0 for one segment: protected paths, hard blocks,
// force confirms — combined worst-part-wins so a segment carrying several
// findings reports all of them. A quote-stripped pass catches dangers hidden
// behind quoting (`bash -c 'rm -rf /'`).
func (e *Engine) evaluateSegment(cmd string) (contract.Verdict, []string) {
	var partials []contract.Verdict
	var fired []string

	if hits := e.rules.PathHits(cmd); len(hits) > 0 {
		v := allowVerdict()
		v.Decision = contract.DecisionBlock
		v.Risk = contract.RiskHigh
		for _, h := range hits {
			v.Reasons = append(v.Reasons, fmtReason(h.RuleID(), h.Reason))
			fired = append(fired, h.RuleID())
		}
		partials = append(partials, v)
	}
	if r := e.rules.FirstHardHit(cmd); r != nil {
		partials = append(partials, blockCritical(r.ID, r.Reason))
		fired = append(fired, r.ID)
	}
	if r := e.rules.FirstForceHit(cmd); r != nil {
		partials = append(partials, reviewHigh(r.ID, r.Reason))
		fired = append(fired, r.ID)
	}
	if stripped := stripQuotes(cmd); stripped != cmd {
		if r := e.rules.FirstHardHit(stripped); r != nil {
			partials = append(partials, blockCritical(r.ID+" (quoted)", r.Reason))
			fired = append(fired, r.ID)
		}
	}
	if len(partials) == 0 {
		return allowVerdict(), fired
	}
	return Combine(partials), fired
}

// evaluateComposition applies the composition rules to a chain fragment —
// a joined pipe pair ("curl http://x | sh") or an invocation segment. All
// hits are reported.
func (e *Engine) evaluateComposition(text string) (contract.Verdict, []string) {
	hits := e.rules.CompositionHits(text)
	if len(hits) == 0 {
		return allowVerdict(), nil
	}
	v := allowVerdict()
	v.Decision = contract.DecisionBlock
	v.Risk = contract.RiskCritical
	var fired []string
	for _, h := range hits {
		v.Reasons = append(v.Reasons, fmtReason(h.ID, h.Reason))
		fired = append(fired, h.ID)
	}
	return v, fired
}

// evaluateOtherTool scans every string value of a non-shell tool input
// against the protected-path globs. Reading OR writing a credential is a
// block; everything else about non-shell tools is Layer 2 territory
// (v0.4.0).
func (e *Engine) evaluateOtherTool(raw json.RawMessage) (contract.Verdict, []string) {
	if len(raw) == 0 {
		return allowVerdict(), nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Unparseable non-shell input is not something Layer 0 can claim —
		// it is left for later layers. Layer 0 stays honest.
		return allowVerdict(), nil
	}
	joined := strings.Join(rules.StringValues(doc), " ")
	if hits := e.rules.PathHits(joined); len(hits) > 0 {
		v := allowVerdict()
		v.Decision = contract.DecisionBlock
		v.Risk = contract.RiskHigh
		var fired []string
		for _, h := range hits {
			v.Reasons = append(v.Reasons, fmtReason(h.RuleID(), h.Reason))
			fired = append(fired, h.RuleID())
		}
		return v, fired
	}
	return allowVerdict(), nil
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
