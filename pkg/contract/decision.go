// Package contract defines Glove's public decision contract.
//
// FROZEN at 1.0.0 (PRD §5.3, §10.4): no field may be added, removed or
// retyped, and no JSON tag may change, without a documented schema-version
// bump and explicit sign-off from the owner. `pkg/contract` is the only
// surface external consumers (adapters, serve clients, tooling) may depend on.
package contract

// Verdict is the outcome of a Glove decision. This struct is the stable
// public contract; treat it as API and version it.
type Verdict struct {
	Decision       Decision `json:"decision"`            // "allow" | "review" | "block"
	Risk           Risk     `json:"risk"`                // "low" | "medium" | "high" | "critical"
	Confidence     float64  `json:"confidence"`          // [0,1], calibrated
	Reasons        []string `json:"reasons"`             // human-readable
	Deterministic  bool     `json:"deterministic"`       // true if Layer 0 or 1 decided
	ModelAvailable bool     `json:"model_available"`     // false if serve was unavailable
	Questions      []QA     `json:"questions,omitempty"` // Layer 2 typed answers
	AuditID        string   `json:"audit_id"`            // UUID
	LatencyMs      int64    `json:"latency_ms"`
}

// QA is one typed Layer 2 question with its calibrated answer.
type QA struct {
	Question   string  `json:"question"`
	Answer     bool    `json:"answer"`
	Confidence float64 `json:"confidence"`
}

// Decision is the three-way outcome. A binary allow/block is a WAF; the
// third way ("review") is what makes a guard usable at agent frequency.
type Decision string

const (
	DecisionAllow  Decision = "allow"
	DecisionReview Decision = "review"
	DecisionBlock  Decision = "block"
)

// Risk is the qualitative severity band accompanying a Decision.
type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)
