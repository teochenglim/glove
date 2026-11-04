package decision

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/rules"
	"github.com/teochenglim/glove/pkg/contract"
)

// Engine evaluates tool calls. v0.1.0: Layer 0 only — every verdict is
// deterministic with confidence 1.0; ModelAvailable is false because serve
// does not exist until v0.4.0 (PRD §7).
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

// Evaluate runs the pipeline over one tool call and stamps the audit ID.
// LatencyMs is measured by the caller (CLI boundary — the only place a wall
// clock is allowed to read).
func (e *Engine) Evaluate(in adapter.HookInput) contract.Verdict {
	v := e.evaluate(in)
	v.AuditID = uuid.NewString()
	return v
}

// shellTools are the tools whose tool_input carries a shell command string.
// Adapter-specific tool names join as adapters land.
var shellTools = map[string]bool{
	"Bash": true,
	"sh":   true,
	"bash": true,
	"zsh":  true,
}

func (e *Engine) evaluate(in adapter.HookInput) contract.Verdict {
	if shellTools[in.ToolName] {
		if cmd, ok := commandOf(in.ToolInput); ok && strings.TrimSpace(cmd) != "" {
			return e.evaluateCommand(cmd)
		}
		return allowVerdict()
	}
	// Non-shell tools (Edit, Write, MCP, ...): Layer 0 only claims protected
	// paths — any string field referencing one blocks the call.
	return e.evaluateOtherTool(in.ToolInput)
}

// Explain recomputes the deterministic hits behind a decision for the CLI's
// --explain output. Layer 1 per-segment explanations arrive in v0.2.0.
func (e *Engine) Explain(in adapter.HookInput) []string {
	v := e.evaluate(in)
	return v.Reasons
}

// evaluateCommand is Layer 0 for a shell command string: protected paths,
// then hard blocks, then force-confirms, combined worst-part-wins so a
// single command carrying several findings reports all of them.
func (e *Engine) evaluateCommand(cmd string) contract.Verdict {
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
		v := allowVerdict()
		v.Decision = contract.DecisionBlock
		v.Risk = contract.RiskCritical
		v.Reasons = append(v.Reasons, fmtReason(r.ID, r.Reason))
		partials = append(partials, v)
	}
	if r := e.rules.FirstForceHit(cmd); r != nil {
		v := allowVerdict()
		v.Decision = contract.DecisionReview
		v.Risk = contract.RiskHigh
		v.Reasons = append(v.Reasons, fmtReason(r.ID, r.Reason))
		partials = append(partials, v)
	}
	if len(partials) == 0 {
		return allowVerdict()
	}
	return Combine(partials)
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
		// it is left for later layers (v0.2.0+). Layer 0 stays honest.
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
