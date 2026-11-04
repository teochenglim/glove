package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/rules"
	"github.com/teochenglim/glove/internal/version"
)

// hookColdStartBudgetMs is the PRD §8 NFR gate: hook cold-start ≤ 15ms,
// measured by `glove doctor --bench hook` (PRD §7 0.1.0) and enforced in CI
// by TestAC_0_1_5_hook_cold_start_budget.
const hookColdStartBudgetMs = 15.0

// benchPayload is a benign allow payload: the measured run covers the full
// hook path (stdin parse → Layer 0 → render) at the lightest workload.
var benchPayload = []byte(`{"tool_name":"Bash","tool_input":{"command":"ls -la"}}`)

func newDoctorCmd() *cobra.Command {
	var bench string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify the glove installation (v0.1.0: status + hook cold-start bench)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if bench == "" {
				return runDoctorStatus(os.Stdout)
			}
			if bench != "hook" {
				return fmt.Errorf("unknown bench target %q (supported: hook)", bench)
			}
			return runHookBench(os.Stdout)
		},
	}
	cmd.Flags().StringVar(&bench, "bench", "", "run a benchmark: --bench hook")
	return cmd
}

func runDoctorStatus(w io.Writer) error {
	fmt.Fprintf(w, "glove          %s\n", version.Version)

	rs, err := rules.LoadBuiltin()
	if err != nil {
		return fmt.Errorf("builtin rules: %w", err)
	}
	fmt.Fprintf(w, "rules          %d hard-block, %d force-confirm, %d protected paths\n",
		len(rs.HardBlock), len(rs.ForceConfirm), len(rs.Protected))

	a, ok := adapter.For(adapter.ClaudeCodeName)
	if !ok {
		return errors.New("claude-code adapter missing")
	}
	installed, cfgPath, err := a.Detect()
	if err != nil {
		return fmt.Errorf("detect claude-code: %w", err)
	}
	if !installed {
		fmt.Fprintf(w, "claude-code    config %s — agent not detected\n", cfgPath)
	} else if claudeHookInstalled(a) {
		fmt.Fprintf(w, "claude-code    config %s — hook entry installed ✓ (%s)\n", cfgPath, a.HookEntry())
	} else {
		fmt.Fprintf(w, "claude-code    config %s — hook entry NOT installed (run `glove install claude-code`)\n", cfgPath)
	}
	fmt.Fprintln(w, "layers         L0 deterministic (L1 v0.2.0, L2 v0.4.0 per PRD §7)")
	return nil
}

func claudeHookInstalled(a adapter.Adapter) bool {
	c, ok := a.(interface{ HookInstalled() bool })
	return ok && c.HookInstalled()
}

// runHookBench measures true cold start: full process spawn of this same
// binary through the hook path, per run.
func runHookBench(w io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	runs := 20
	if v := os.Getenv("GLOVE_DOCTOR_BENCH_RUNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 3 {
			runs = n
		}
	}
	samples, err := benchHook(exe, runs)
	if err != nil {
		return err
	}
	return writeBenchReport(w, samples, hookColdStartBudgetMs)
}

// benchHook spawns `exe hook claude-code` runs times with the benign
// payload and returns per-run wall times in ms.
func benchHook(exe string, runs int) ([]float64, error) {
	if runs < 3 {
		runs = 3
	}
	var ms []float64
	for i := 0; i < runs; i++ {
		cmd := exec.Command(exe, "hook", adapter.ClaudeCodeName)
		cmd.Stdin = bytes.NewReader(benchPayload)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		t0 := time.Now()
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("hook run %d failed: %w", i+1, err)
		}
		ms = append(ms, float64(time.Since(t0).Microseconds())/1000.0)
	}
	return ms, nil
}

// benchStats returns min, median (lower), p95, max of the samples.
func benchStats(ms []float64) (min, median, p95, max float64) {
	sorted := append([]float64(nil), ms...)
	sort.Float64s(sorted)
	min = sorted[0]
	max = sorted[len(sorted)-1]
	median = sorted[(len(sorted)-1)/2]
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	p95 = sorted[idx]
	return min, median, p95, max
}

// writeBenchReport prints the measured numbers against the §8 budget and
// fails the process (ExitError 1) when the median exceeds it — this is the
// CI performance gate.
func writeBenchReport(w io.Writer, ms []float64, budget float64) error {
	if len(ms) == 0 {
		return errors.New("no bench samples")
	}
	min, median, p95, max := benchStats(ms)
	fmt.Fprintf(w, "hook cold-start over %d runs: min %.1fms  median %.1fms  p95 %.1fms  max %.1fms\n",
		len(ms), min, median, p95, max)
	if median <= budget {
		fmt.Fprintf(w, "budget: %.1fms (median) — PASS\n", budget)
		return nil
	}
	fmt.Fprintf(w, "budget: %.1fms (median) — FAIL (measured median %.1fms exceeds it)\n", budget, median)
	return ExitError(1)
}
