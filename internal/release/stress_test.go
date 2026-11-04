package release

import (
	"os"
	"testing"
)

// requireStress gates build-heavy tests behind GLOVE_STRESS=1 (`make
// stress`) — see internal/cli/stress_test.go for the rationale. CI's
// cross-compile job covers the compile proof for the matrix targets.
func requireStress(t *testing.T) {
	t.Helper()
	if os.Getenv("GLOVE_STRESS") == "" {
		t.Skip("stress test — run via `make stress` (GLOVE_STRESS=1)")
	}
}
