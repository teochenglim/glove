// Package cli wires the frozen PRD §6 command surface onto the engine,
// policy and audit. Subcommands land with their releases: hook/check/
// install/doctor in v0.1.0 (+doctor bench v0.2.0 era), audit + policy in
// v0.3.0, serve in v0.4.0, list in v0.5.0.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/decision"
	"github.com/teochenglim/glove/internal/policy"
	"github.com/teochenglim/glove/internal/version"
)

// ExitError carries a process exit code through cobra to main (PRD §6:
// hook exits 0/2; everything else is a failure).
type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", int(e))
}

// runtime is the resolved policy plus the engine composed from it. Every
// command builds one at startup — that per-process load is what makes
// policy edits enforceable with nothing to restart (PRD §7 0.3.0).
type runtime struct {
	Engine  *decision.Engine
	Policy  *policy.Policy
	Sources policy.Sources
}

// loadRuntime resolves the effective policy and composes the engine. An
// explicit policyPath (`glove check --policy`, `glove policy validate`)
// replaces discovery.
func loadRuntime(policyPath string) (*runtime, error) {
	var p *policy.Policy
	var src policy.Sources
	var err error
	if policyPath != "" {
		p, err = policy.LoadFile(policyPath)
		src = policy.Sources{Project: policyPath}
	} else {
		p, src, err = policy.Load()
	}
	if err != nil {
		return nil, err
	}
	eng, err := decision.NewFromPolicy(p)
	if err != nil {
		return nil, fmt.Errorf("compose engine: %w", err)
	}
	return &runtime{Engine: eng, Policy: p, Sources: src}, nil
}

// NewRootCommand builds the glove CLI.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "glove",
		Short: "The LLM has a hand — tool calling. Glove is what it wears to act safely.",
		Long: `Glove is a single-binary local guard for AI agent tool calls.

It sits between the agent's intent and the hand's motion: deterministic
rules first (zero latency), shell-aware chain splitting second, a local
decision model only for the gray areas. The guard only ever tightens.
Zero network calls on the decision path.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newVersionCmd(),
		newCheckCmd(),
		newHookCmd(),
		newInstallCmd(),
		newDoctorCmd(),
		newAuditCmd(),
		newPolicyCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the glove version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Version)
		},
	}
}
