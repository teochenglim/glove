package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/audit"
	"github.com/teochenglim/glove/internal/decision"
	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/internal/policy"
	"github.com/teochenglim/glove/internal/version"
	"github.com/teochenglim/glove/pkg/contract"
)

// HookOptions carries the hook-path flags (PRD §7 0.3.0 --audit-verbose).
type HookOptions struct {
	AuditVerbose bool
}

// RunHook is the PreToolUse entry point (PRD §3.2): read the agent's stdin
// JSON, evaluate, render the agent's decision JSON to stdout, and return the
// process exit code (§6): 0 allow/review, 2 block.
//
// Fail-closed (PRD §8 — "never return open on internal error"): policy load
// failure, engine load failure, unknown agent, unparseable stdin — every
// internal error is reported as a deny (exit 2), never as a silent open gate.
func RunHook(stdout, stderr io.Writer, agentName string, stdin io.Reader) int {
	return RunHookWith(stdout, stderr, agentName, stdin, HookOptions{})
}

func RunHookWith(stdout, stderr io.Writer, agentName string, stdin io.Reader, opts HookOptions) int {
	start := time.Now()

	writeDeny := func(reason string) int {
		stdout.Write(denyJSON(reason))
		stdout.Write([]byte("\n"))
		fmt.Fprintf(stderr, "glove: %s\n", reason)
		return 2
	}

	a, ok := adapter.For(agentName)
	if !ok {
		return writeDeny(fmt.Sprintf("unknown agent %q — failing closed", agentName))
	}
	rt, err := loadRuntime("")
	if err != nil {
		return writeDeny(fmt.Sprintf("policy: %v — failing closed", err))
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return writeDeny(fmt.Sprintf("read stdin: %v — failing closed", err))
	}
	in, err := a.ParseHookInput(raw)
	if err != nil {
		return writeDeny(fmt.Sprintf("invalid hook input: %v — failing closed", err))
	}

	v := rt.Engine.Evaluate(in)
	v.ModelAvailable = false

	// Layer 2 (PRD §7 0.4.0): standard mode consults serve; fast/offline skip
	// it. The deterministic verdict always stands on its own — Laya can only
	// tighten (§4.2, via Combine). Serve unreachable → the offline path: bare
	// allows escalate per [general] on_model_unavailable (§3.2 fail-closed).
	if rt.Policy.General.Mode == policy.ModeStandard {
		timeout := time.Duration(rt.Policy.Laya.HookTimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = policy.DefaultHookTimeoutMs * time.Millisecond
		}
		client := decision.NewLayaClient(rt.Policy.Laya.Endpoint, timeout)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		l2, err := client.Ask(ctx, in)
		cancel()
		if err == nil {
			v = decision.Combine([]contract.Verdict{v, l2})
		} else if v.Decision == contract.DecisionAllow {
			if rt.Policy.General.OnModelUnavailable == "block" {
				v.Decision = contract.DecisionBlock
				v.Risk = contract.RiskHigh
				v.Reasons = append(v.Reasons, "model unavailable — undecidable call fails closed per policy (§3.2)")
			} else {
				v.Decision = contract.DecisionReview
				v.Risk = contract.RiskMedium
				v.Reasons = append(v.Reasons, "model unavailable — undecidable call defaults to review (§3.2)")
			}
		}
	}

	v.LatencyMs = time.Since(start).Milliseconds()
	out, err := a.RenderVerdict(v)
	if err != nil {
		return writeDeny(fmt.Sprintf("render verdict: %v — failing closed", err))
	}
	stdout.Write(out)
	stdout.Write([]byte("\n"))

	// Audit only after the decision is delivered (PRD §4.1: the writer never
	// blocks the decision path). Failures are stderr warnings — the guard
	// must not turn an audit hiccup into a wrong or lost decision.
	writeAudit(stderr, opts, rt, a.Name(), in, v)

	if v.Decision == contract.DecisionBlock {
		return 2
	}
	return 0
}

// writeAudit records the decision (PRD §5.4). The default record carries
// zero plaintext: a SHA-256 input_hash only; --audit-verbose adds the
// redacted input.
func writeAudit(stderr io.Writer, opts HookOptions, rt *runtime, agent string, in adapter.HookInput, v contract.Verdict) {
	dir := paths.DataDir()
	if dir == "" {
		return
	}
	detectPolicyReload(stderr, dir, rt)

	hash := sha256.Sum256(in.ToolInput)
	rec := audit.Record{
		AuditID:        v.AuditID,
		Agent:          agent,
		SessionID:      in.SessionID,
		Tool:           in.ToolName,
		InputHash:      hex.EncodeToString(hash[:]),
		Decision:       string(v.Decision),
		Risk:           string(v.Risk),
		Confidence:     v.Confidence,
		Deterministic:  v.Deterministic,
		ModelAvailable: v.ModelAvailable,
		Reasons:        v.Reasons,
		Questions:      v.Questions,
		LatencyMs:      v.LatencyMs,
		GloveVersion:   version.Version,
	}
	if opts.AuditVerbose && len(in.ToolInput) > 0 {
		red := audit.Redact(string(in.ToolInput))
		rec.InputRedacted = &red
	}
	if err := audit.NewWriter(dir).Write(rec); err != nil {
		fmt.Fprintf(stderr, "glove: audit write failed (decision already delivered): %v\n", err)
	}
}

// detectPolicyReload writes a policy_reload event when the resolved policy
// hash differs from the previous hook invocation (PRD §7 0.3.0). The hash
// state lives beside the audit log; the first invocation establishes it
// without emitting an event. Since every hook process re-resolves policy at
// startup, an edit is enforced on the next tool call with nothing to restart.
func detectPolicyReload(stderr io.Writer, dir string, rt *runtime) {
	hash, err := rt.Policy.Hash()
	if err != nil {
		return
	}
	state := filepath.Join(dir, ".policy_state")
	prev, readErr := os.ReadFile(state)
	switch {
	case readErr == nil && string(prev) != hash:
		// Policy changed since the last invocation: emit the reload event.
		w := audit.NewWriter(dir)
		ev := audit.Record{
			AuditID:      uuid.NewString(),
			Event:        audit.EventPolicyReload,
			Reasons:      []string{"policy changed — " + rt.Sources.Describe()},
			GloveVersion: version.Version,
		}
		if err := w.Write(ev); err != nil {
			fmt.Fprintf(stderr, "glove: audit write failed (policy_reload event): %v\n", err)
		}
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(state, []byte(hash), 0o600)
	case readErr == nil:
		// Unchanged: nothing to emit.
	case os.IsNotExist(readErr):
		// First invocation establishes the state without an event.
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(state, []byte(hash), 0o600)
	default:
		fmt.Fprintf(stderr, "glove: policy state read failed: %v\n", readErr)
	}
}

// denyJSON renders a fail-closed deny verdict in the Claude Code envelope —
// the only stdout consumer at that point is the hook protocol.
func denyJSON(reason string) []byte {
	v := contract.Verdict{
		Decision:       contract.DecisionBlock,
		Risk:           contract.RiskCritical,
		Confidence:     1,
		Deterministic:  true,
		ModelAvailable: false,
		Reasons:        []string{reason},
		AuditID:        uuid.NewString(),
	}
	a, ok := adapter.For(adapter.ClaudeCodeName)
	if !ok {
		return []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"glove internal error"}}`)
	}
	out, err := a.RenderVerdict(v)
	if err != nil {
		return []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"glove internal error"}}`)
	}
	return out
}

func newHookCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "hook <agent>",
		Short: "PreToolUse hook entry point — invoked by the agent, not by you",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := RunHookWith(os.Stdout, os.Stderr, args[0], os.Stdin, HookOptions{AuditVerbose: verbose})
			if code != 0 {
				return ExitError(code)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&verbose, "audit-verbose", false, "record the redacted input alongside the input hash (default record carries the hash only)")
	return cmd
}
