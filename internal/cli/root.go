// Package cli wires the frozen PRD §6 command surface onto the engine and
// adapters. Subcommands land with their releases: hook/check/install/doctor
// in v0.1.0, audit in v0.3.0, serve in v0.4.0, policy in v0.7.0, list in
// v0.6.0.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/decision"
	"github.com/teochenglim/glove/internal/version"
)

// ExitError carries a process exit code through cobra to main (PRD §6:
// hook exits 0/2; everything else is a failure).
type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", int(e))
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

// loadEngine builds the decision engine from the embedded builtin rules.
// The single construction point keeps "which rule set am I running" honest.
func loadEngine() (*decision.Engine, error) {
	return decision.NewBuiltin()
}
