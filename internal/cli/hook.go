package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

// RunHook is the PreToolUse entry point (PRD §3.2): read the agent's stdin
// JSON, evaluate, render the agent's decision JSON to stdout, and return the
// process exit code (§6): 0 allow/review, 2 block.
//
// Fail-closed (PRD §8 — "never return open on internal error"): engine load
// failure, unknown agent, unparseable stdin — every internal error is
// reported as a deny (exit 2), never as a silent open gate.
func RunHook(stdout, stderr io.Writer, agentName string, stdin io.Reader) int {
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
	eng, err := loadEngine()
	if err != nil {
		return writeDeny(fmt.Sprintf("engine load failed: %v — failing closed", err))
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return writeDeny(fmt.Sprintf("read stdin: %v — failing closed", err))
	}
	in, err := a.ParseHookInput(raw)
	if err != nil {
		return writeDeny(fmt.Sprintf("invalid hook input: %v — failing closed", err))
	}

	v := eng.Evaluate(in)
	v.LatencyMs = time.Since(start).Milliseconds()
	out, err := a.RenderVerdict(v)
	if err != nil {
		return writeDeny(fmt.Sprintf("render verdict: %v — failing closed", err))
	}
	stdout.Write(out)
	stdout.Write([]byte("\n"))
	if v.Decision == contract.DecisionBlock {
		return 2
	}
	return 0
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
	return &cobra.Command{
		Use:   "hook <agent>",
		Short: "PreToolUse hook entry point — invoked by the agent, not by you",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := RunHook(os.Stdout, os.Stderr, args[0], os.Stdin)
			if code != 0 {
				return ExitError(code)
			}
			return nil
		},
	}
}
