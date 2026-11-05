// Package audit writes and reads glove's decision log (PRD §5.4, §7 0.3.0).
// Default posture: zero plaintext command bytes — the record carries a
// SHA-256 input_hash only; raw (redacted) input appears solely under
// --audit-verbose.
//
// Since v0.7.0 (schema version 2) every record is part of a tamper-evident
// hash chain (prev_hash → record_hash, ADR-adjacent: PRD §7 0.7.0) and
// carries machine identity + the MITRE ATT&CK tags of the rules that fired,
// so a SIEM can join, filter and trust the trail (docs/siem.md).
package audit

import (
	"time"

	"github.com/teochenglim/glove/pkg/contract"
)

// Event values for non-decision records (Event is "" for decisions).
const (
	EventPolicyReload = "policy_reload"
	EventSinkError    = "sink_error"   // delivery attempt failed (AC0.7.5)
	EventSinkDropped  = "sink_dropped" // bounded queue overflowed (AC0.7.5)
)

// AuditSchemaVersion is the current audit JSONL schema version (PRD §5.4):
// 1 = pre-0.7.0 records (no chain fields), 2 = v0.7.0 (chain + identity +
// techniques). Bumping it requires a release-file note; the schema freezes
// at 1.0.0 alongside contract.Verdict.
const AuditSchemaVersion = 2

// Record is one immutable audit line (PRD §5.4). Treat the JSONL schema as
// API: it freezes at 1.0.0 alongside contract.Verdict.
type Record struct {
	Ts             time.Time     `json:"ts"`
	AuditID        string        `json:"audit_id"`
	Agent          string        `json:"agent"`
	SessionID      string        `json:"session_id,omitempty"`
	Tool           string        `json:"tool"`
	InputHash      string        `json:"input_hash"`               // SHA-256, hex, over the raw tool_input
	InputRedacted  *string       `json:"input_redacted,omitempty"` // only in --audit-verbose
	Decision       string        `json:"decision"`
	Risk           string        `json:"risk"`
	Confidence     float64       `json:"confidence"`
	Deterministic  bool          `json:"deterministic"`
	ModelAvailable bool          `json:"model_available"`
	Reasons        []string      `json:"reasons"`
	Questions      []contract.QA `json:"questions,omitempty"` // Layer 2 typed answers (v0.4.0)
	LatencyMs      int64         `json:"latency_ms"`
	GloveVersion   string        `json:"glove_version"`
	Event          string        `json:"event,omitempty"` // "" for decisions; EventPolicyReload etc.

	// --- v0.7.0 fields (schema version 2, PRD §5.4) ---

	SchemaVersion int      `json:"schema_version"` // AuditSchemaVersion; 0 when read from a legacy record
	Host          string   `json:"host,omitempty"`
	User          string   `json:"user,omitempty"`
	MachineID     string   `json:"machine_id,omitempty"`  // sha256(host+"/"+user), first 16 hex — stable join key
	Techniques    []string `json:"techniques,omitempty"`  // MITRE ATT&CK tags inherited from the fired rules
	PrevHash      string   `json:"prev_hash,omitempty"`   // record_hash of the previous record ("" at chain start)
	RecordHash    string   `json:"record_hash,omitempty"` // sha256 over this record with RecordHash zeroed
}
