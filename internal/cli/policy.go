package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/policy"
)

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Policy file operations (PRD §5.5)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newPolicyValidateCmd())
	return cmd
}

// newPolicyValidateCmd implements `glove policy validate <file>`: TOML
// syntax errors carry the line and caret from the parser; schema errors
// (bad enums, bands, regexps) name the offending key path. Exit 1 on any
// error — validation is how users iterate before a policy goes live.
func newPolicyValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a policy.toml file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("read policy %s: %w", path, err)
			}
			p, err := policy.LoadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "%s: valid (mode=%s, on_model_unavailable=%s)\n", path, p.General.Mode, p.General.OnModelUnavailable)
			return nil
		},
	}
}
