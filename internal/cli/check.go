package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

func newCheckCmd() *cobra.Command {
	var explain, offline bool
	cmd := &cobra.Command{
		Use:   `check "<command>"`,
		Short: "Evaluate a command through the decision engine and print the verdict",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			eng, err := loadEngine()
			if err != nil {
				return fmt.Errorf("load engine: %w", err)
			}
			raw, err := json.Marshal(map[string]string{"command": command})
			if err != nil {
				return err
			}
			in := adapter.HookInput{
				Agent:     "cli",
				Event:     "Check",
				ToolName:  "Bash",
				ToolInput: raw,
			}
			start := time.Now()
			v := eng.Evaluate(in)
			v.LatencyMs = time.Since(start).Milliseconds()

			printVerdict(os.Stdout, v)
			if explain {
				hits := eng.Explain(in)
				if len(hits) == 0 {
					fmt.Println("  no rule hits (Layer 1 per-segment explanations arrive in v0.2.0)")
				}
				for _, h := range hits {
					fmt.Fprintf(os.Stdout, "  hit: %s\n", h)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&explain, "explain", false, "show the rule hits behind the decision")
	cmd.Flags().BoolVar(&offline, "offline", false, "force offline evaluation (0.1.0 always is; serve lands in v0.4.0)")
	return cmd
}

func printVerdict(w io.Writer, v contract.Verdict) {
	model := "unavailable"
	if v.ModelAvailable {
		model = "available"
	}
	fmt.Fprintf(w, "decision       %s\n", v.Decision)
	fmt.Fprintf(w, "risk           %s\n", v.Risk)
	fmt.Fprintf(w, "confidence     %.2f\n", v.Confidence)
	fmt.Fprintf(w, "deterministic  %t\n", v.Deterministic)
	fmt.Fprintf(w, "model          %s\n", model)
	fmt.Fprintf(w, "latency        %dms\n", v.LatencyMs)
	if len(v.Reasons) > 0 {
		fmt.Fprintln(w, "reasons:")
		for _, r := range v.Reasons {
			fmt.Fprintf(w, "  - %s\n", r)
		}
	}
}
