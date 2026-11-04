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
	"github.com/teochenglim/glove/internal/decision"
	"github.com/teochenglim/glove/pkg/contract"
)

func newCheckCmd() *cobra.Command {
	var explain, offline bool
	var policyPath string
	cmd := &cobra.Command{
		Use:   `check "<command>"`,
		Short: "Evaluate a command through the decision engine and print the verdict",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			rt, err := loadRuntime(policyPath)
			if err != nil {
				return err
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
			v, segments := rt.Engine.EvaluateDetailed(in)
			v.LatencyMs = time.Since(start).Milliseconds()

			printVerdict(os.Stdout, v)
			fmt.Fprintf(os.Stdout, "policy         %s (%s)\n", rt.Policy.General.Mode, rt.Sources.Describe())
			if explain {
				printSegments(os.Stdout, segments)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&explain, "explain", false, "show the per-segment decomposition and rule hits behind the decision")
	cmd.Flags().BoolVar(&offline, "offline", false, "force offline evaluation (serve arrives in v0.4.0; Layer 0+1 always run offline)")
	cmd.Flags().StringVar(&policyPath, "policy", "", "evaluate against an explicit policy file instead of the discovered one (PRD §7 0.3.0)")
	return cmd
}

// printSegments renders the Layer 1 decomposition: one line per segment,
// then any pipe-into-shell pair findings (only visible as a pair).
func printSegments(w io.Writer, segs []decision.SegmentVerdict) {
	if len(segs) == 0 {
		fmt.Fprintln(w, "segments:      none (no rule hits)")
		return
	}
	fmt.Fprintln(w, "segments:")
	for i, s := range segs {
		via := ""
		if s.Via != "" {
			via = " (" + s.Via + ")"
		}
		compound := ""
		if s.Compound != "" {
			compound = " [" + s.Compound + "]"
		}
		fmt.Fprintf(w, "  [%d] %s%s%s → %s\n", i+1, s.Text, via, compound, s.Decision)
		for _, r := range s.Reasons {
			fmt.Fprintf(w, "      - %s\n", r)
		}
		if s.Pair != "" {
			fmt.Fprintf(w, "      pair %q → %s\n", s.Pair, s.PairRisk)
			for _, r := range s.PairWhy {
				fmt.Fprintf(w, "      - %s\n", r)
			}
		}
	}
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
