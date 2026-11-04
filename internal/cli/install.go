package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/decision"
	"github.com/teochenglim/glove/pkg/contract"
)

func newInstallCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install <agent>",
		Short: "Write the glove hook into an agent's config (shows a diff, backs up, idempotent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]
			a, ok := adapter.For(name)
			if !ok {
				return fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(adapter.Names(), ", "))
			}
			if err := a.Install(context.Background(), adapter.InstallOptions{DryRun: dryRun, Writer: out}); err != nil {
				return err
			}
			if dryRun {
				return nil
			}
			// PRD §7 0.1.0: verify the install with two test calls.
			rt, err := loadRuntime("")
			if err != nil {
				return fmt.Errorf("load policy: %w", err)
			}
			okAllow := testCall(out, rt.Engine, "ls -la", contract.DecisionAllow)
			okBlock := testCall(out, rt.Engine, "rm -rf /", contract.DecisionBlock)
			if !okAllow || !okBlock {
				fmt.Fprintln(os.Stderr, "glove: install verification failed — the hook was written but the engine did not behave; run `glove doctor`")
				return ExitError(1)
			}
			fmt.Fprintln(out, "install verified: allow and block paths both behave.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the config diff only, write nothing")
	return cmd
}

func testCall(w io.Writer, eng *decision.Engine, command string, want contract.Decision) bool {
	raw, _ := json.Marshal(map[string]string{"command": command})
	in := adapter.HookInput{
		Agent:     "cli",
		Event:     "InstallCheck",
		ToolName:  "Bash",
		ToolInput: raw,
	}
	v := eng.Evaluate(in)
	mark := "PASS"
	if v.Decision != want {
		mark = "FAIL"
	}
	fmt.Fprintf(w, "  test call: %-10q -> %-5s (want %s)  %s\n", command, v.Decision, want, mark)
	return v.Decision == want
}
