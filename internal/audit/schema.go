// Package audit writes and reads glove's decision log (PRD §5.4, §7 0.3.0).
// Default posture: zero plaintext command bytes — the record carries a
// SHA-256 input_hash only; raw (redacted) input appears solely under
// --audit-verbose.
package audit

import "time"

// Event values for non-decision records (Event is "" for decisions).
const (
	EventPolicyReload = "policy_reload"
)

// Record is one immutable audit line (PRD §5.4). Treat the JSONL schema as
// API: it freezes at 1.0.0 alongside contract.Verdict.
type Record struct {
	Ts             time.Time `json:"ts"`
	AuditID        string    `json:"audit_id"`
	Agent          string    `json:"agent"`
	SessionID      string    `json:"session_id,omitempty"`
	Tool           string    `json:"tool"`
	InputHash      string    `json:"input_hash"`               // SHA-256, hex, over the raw tool_input
	InputRedacted  *string   `json:"input_redacted,omitempty"` // only in --audit-verbose
	Decision       string    `json:"decision"`
	Risk           string    `json:"risk"`
	Confidence     float64   `json:"confidence"`
	Deterministic  bool      `json:"deterministic"`
	ModelAvailable bool      `json:"model_available"`
	Reasons        []string  `json:"reasons"`
	LatencyMs      int64     `json:"latency_ms"`
	GloveVersion   string    `json:"glove_version"`
	Event          string    `json:"event,omitempty"` // "" for decisions; EventPolicyReload etc.
}
