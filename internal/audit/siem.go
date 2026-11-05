package audit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teochenglim/glove/internal/version"
)

// SIEM export formats (AC0.7.4): CEF (ArcSight-class consumers) and ECS
// (Elastic-class consumers), alongside the existing jsonl/csv. Both are
// stateless renderings of a Record — docs/siem.md carries the ingestion
// recipes and the field mapping table.

// CEF renders one record as a Common Event Format line (CEF:0). Severity is
// 0–10: allow=2, review=5, block=8, +2 for critical risk (capped at 10).
func CEF(r Record) string {
	sev := 2
	switch r.Decision {
	case "review":
		sev = 5
	case "block":
		sev = 8
	}
	if r.Risk == "critical" {
		sev += 2
		if sev > 10 {
			sev = 10
		}
	}
	name := ""
	if len(r.Reasons) > 0 {
		name = r.Reasons[0]
	}
	var ext strings.Builder
	ext.WriteString("start=" + strconv.FormatInt(r.Ts.UnixMilli(), 10))
	ext.WriteString(" dhost=" + cefValue(r.Host))
	ext.WriteString(" suser=" + cefValue(r.User))
	ext.WriteString(" app=" + cefValue(r.Agent))
	if r.InputHash != "" {
		ext.WriteString(" fileHash=" + r.InputHash)
	}
	if r.SessionID != "" {
		ext.WriteString(" cs1Label=session cs1=" + cefValue(r.SessionID))
	}
	if len(r.Techniques) > 0 {
		ext.WriteString(" cs2Label=techniques cs2=" + cefValue(strings.Join(r.Techniques, ",")))
	}
	if r.MachineID != "" {
		ext.WriteString(" cs3Label=machine_id cs3=" + r.MachineID)
	}
	if len(r.Reasons) > 0 {
		ext.WriteString(" msg=" + cefValue(strings.Join(r.Reasons, "; ")))
	}
	return fmt.Sprintf("CEF:0|glove|glove|%s|%s|%s|%d|%s",
		version.Version, cefHeader(r.Tool), cefHeader(name), sev, ext.String())
}

// cefHeader escapes a CEF header field: backslash and pipe.
func cefHeader(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "|", "\\|")
}

// cefValue escapes a CEF extension value: backslash, equals and newline.
func cefValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "=", "\\=")
	return strings.ReplaceAll(s, "\n", "\\n")
}

// ECS renders one record as an Elastic Common Schema JSON document
// (ECS 8.11 field names). The raw glove decision stays under `glove.*` —
// outcome alone loses the review/block distinction.
func ECS(r Record) ([]byte, error) {
	outcome := "success"
	switch r.Decision {
	case "block":
		outcome = "failure"
	case "review":
		outcome = "unknown"
	}
	event := map[string]any{
		"kind":      "alert",
		"dataset":   "glove.audit",
		"module":    "glove",
		"id":        r.AuditID,
		"action":    r.Tool,
		"outcome":   outcome,
		"risk":      r.Risk,
		"duration":  r.LatencyMs * 1_000_000, // ECS durations are nanoseconds
		"reference": "/glove/audit/" + r.AuditID,
	}
	if r.Event != "" {
		event["kind"] = "event"
		event["action"] = r.Event
	}
	glove := map[string]any{
		"decision":        r.Decision,
		"risk":            r.Risk,
		"confidence":      r.Confidence,
		"deterministic":   r.Deterministic,
		"model_available": r.ModelAvailable,
		"version":         r.GloveVersion,
		"input_hash":      r.InputHash,
		"schema_version":  r.SchemaVersion,
		"reasons":         r.Reasons,
		"machine_id":      r.MachineID,
		"latency_ms":      r.LatencyMs,
	}
	if r.SessionID != "" {
		glove["session_id"] = r.SessionID
	}
	if len(r.Techniques) > 0 {
		glove["techniques"] = r.Techniques
	}
	doc := map[string]any{
		"@timestamp": r.Ts.Format(time.RFC3339Nano),
		"ecs":        map[string]any{"version": "8.11.0"},
		"event":      event,
		"agent":      map[string]any{"name": r.Host, "type": "glove"},
		"user":       map[string]any{"name": r.User},
		"glove":      glove,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode ECS record: %w", err)
	}
	return b, nil
}
