// hookwire.go — the stdin/stdout wire shapes of the JSON-stdin agents
// (claude-code, codex, cursor, gemini). Codex's PreToolUse protocol is
// Claude-Code-compatible (verified against codex-rs/hooks schemas, 2026-11);
// cursor and gemini share the tool_name/tool_input/cwd input vocabulary but
// speak their own decision envelopes. Every map here is agent-pinned: one
// protocol change touches one function in one file (PRD §11).

package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teochenglim/glove/pkg/contract"
)

// parseClaudeStyleInput decodes the common JSON-stdin vocabulary:
// hook_event_name / tool_name / tool_input / session_id / cwd, with
// cursor's conversation_id as a session fallback.
func parseClaudeStyleInput(agent string, raw []byte) (HookInput, error) {
	var wire struct {
		Event          string          `json:"hook_event_name"`
		ToolName       string          `json:"tool_name"`
		ToolInput      json.RawMessage `json:"tool_input"`
		SessionID      string          `json:"session_id"`
		ConversationID string          `json:"conversation_id"`
		CWD            string          `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return HookInput{}, fmt.Errorf("parse %s hook input: %w", agent, err)
	}
	session := wire.SessionID
	if session == "" {
		session = wire.ConversationID
	}
	return HookInput{
		Agent:     agent,
		Event:     wire.Event,
		ToolName:  wire.ToolName,
		ToolInput: wire.ToolInput,
		SessionID: session,
		CWD:       wire.CWD,
	}, nil
}

// permissionDecisionBody is the Claude-Code-shaped decision envelope, reused
// verbatim by codex (schemas identical: hookSpecificOutput.
// permissionDecision ∈ allow|deny|ask — PRD §3.1 vocabulary).
type permissionDecisionBody struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             any    `json:"updatedInput,omitempty"`
}

type permissionDecisionOut struct {
	HookSpecificOutput permissionDecisionBody `json:"hookSpecificOutput"`
}

// threeWay maps the frozen contract onto the allow/ask/deny vocabulary
// shared by claude-code and codex (PRD §3.1).
func threeWay(d contract.Decision) (string, error) {
	switch d {
	case contract.DecisionAllow:
		return "allow", nil
	case contract.DecisionReview:
		return "ask", nil
	case contract.DecisionBlock:
		return "deny", nil
	default:
		return "", fmt.Errorf("unknown decision %q", d)
	}
}

// renderPermissionDecision renders the claude-code/codex envelope.
func renderPermissionDecision(event string, v contract.Verdict) ([]byte, error) {
	dec, err := threeWay(v.Decision)
	if err != nil {
		return nil, err
	}
	out := permissionDecisionOut{HookSpecificOutput: permissionDecisionBody{
		HookEventName:            event,
		PermissionDecision:       dec,
		PermissionDecisionReason: strings.Join(v.Reasons, "; "),
	}}
	return json.Marshal(out)
}

// --- cursor ---

// cursorOut is the Cursor hooks decision envelope (cursor.com/docs/hooks):
// {"permission":"allow|deny|ask","user_message":...}. Deviation, documented
// in docs/install/cursor.md: review renders as deny, not ask — Cursor does
// not enforce "ask" on preToolUse today, and an unenforced ask would let a
// glove review sail through (the one thing the guard may never do is
// loosen). The reason always names it a review so the human sees why.
type cursorOut struct {
	Permission  string `json:"permission"`
	UserMessage string `json:"user_message,omitempty"`
}

func renderCursorVerdict(v contract.Verdict) ([]byte, error) {
	perm, err := threeWay(v.Decision)
	if err != nil {
		return nil, err
	}
	if v.Decision == contract.DecisionReview {
		perm = "deny" // tighten-only: cursor's preToolUse does not enforce "ask"
	}
	msg := strings.Join(v.Reasons, "; ")
	if v.Decision == contract.DecisionReview {
		msg = "glove review — blocked pending your confirmation: " + msg
	}
	out := cursorOut{Permission: perm, UserMessage: msg}
	return json.Marshal(out)
}

// --- gemini ---

// geminiOut is the Gemini CLI BeforeTool decision envelope
// (docs/hooks/reference.md): {"decision":"deny","reason":...}. Allow renders
// as empty output — the docs fail open on non-JSON stdout, and empty stdout
// is unambiguous "proceed". Block additionally rides the §6 exit code 2
// (system block, stderr as reason).
type geminiOut struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func renderGeminiVerdict(v contract.Verdict) ([]byte, error) {
	switch v.Decision {
	case contract.DecisionAllow:
		return []byte{}, nil
	case contract.DecisionReview, contract.DecisionBlock:
		reason := strings.Join(v.Reasons, "; ")
		if v.Decision == contract.DecisionReview {
			reason = "glove review — blocked pending your confirmation: " + reason
		}
		return json.Marshal(geminiOut{Decision: "deny", Reason: reason})
	default:
		return nil, fmt.Errorf("unknown decision %q", v.Decision)
	}
}
