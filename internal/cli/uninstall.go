package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
)

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <agent>",
		Short: "Remove the glove hook from an agent's config (idempotent, backs up)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]
			a, ok := adapter.For(name)
			if !ok {
				return fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(adapter.Names(), ", "))
			}
			if err := a.Uninstall(context.Background()); err != nil {
				return err
			}
			fmt.Fprintf(out, "uninstalled %s — run `glove doctor` to confirm\n", name)
			return nil
		},
	}
}
