package cli

import (
	"os"
	"testing"
)

// requireStress gates load- and timing-heavy tests behind GLOVE_STRESS=1
// (`make stress`). Shared CI runners turn wall-clock budgets into flake
// sources — a 2-vCPU runner measured the same 10k query at 335ms that a
// laptop passes at ~50ms — so the §8 budgets are verified on release
// hardware (the maintainer's machine, `make stress` before a release)
// instead of gate-keeping every push. CI keeps the deterministic suite
// (`make check`); regressions in the load class are caught by running
// stress before tagging, and by soak.sh (benchmarks/).
func requireStress(t *testing.T) {
	t.Helper()
	if os.Getenv("GLOVE_STRESS") == "" {
		t.Skip("stress test — run via `make stress` (GLOVE_STRESS=1)")
	}
}
