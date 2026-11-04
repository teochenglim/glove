package cli

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildGlove compiles the real binary for bench-style tests (the cold-start
// budget is a property of the binary, not of the test process).
func buildGlove(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "glove")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/glove")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// TestAC_0_1_5_hook_cold_start_budget — AC0.1.5: true cold start of the
// hook path (full process spawn → stdin parse → Layer 0 → render) stays
// within the PRD §8 budget of 15ms. This test is the CI performance gate.
func TestAC_0_1_5_hook_cold_start_budget(t *testing.T) {
	bin := buildGlove(t)
	samples, err := benchHook(bin, 12)
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	var buf bytes.Buffer
	if err := writeBenchReport(&buf, samples, hookColdStartBudgetMs); err != nil {
		t.Fatalf("cold start over budget:\n%s\nerr: %v", buf.String(), err)
	}
}

// TestAC_0_1_5_doctor_bench_reports — AC0.1.5 other half: the bench report
// carries the measured numbers and enforces the budget on the median.
func TestAC_0_1_5_doctor_bench_reports(t *testing.T) {
	t.Run("pass_reports_numbers", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBenchReport(&buf, []float64{4.2, 3.9, 5.1, 4.0, 4.4}, 15.0); err != nil {
			t.Fatalf("expected PASS, got err %v", err)
		}
		out := buf.String()
		for _, want := range []string{"median", "PASS", "ms"} {
			if !bytes.Contains([]byte(out), []byte(want)) {
				t.Errorf("report missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("fail_exceeds_budget", func(t *testing.T) {
		var buf bytes.Buffer
		err := writeBenchReport(&buf, []float64{20.0, 21.0, 19.5}, 15.0)
		if err == nil {
			t.Fatal("expected FAIL (median 20ms > 15ms budget)")
		}
		if !errors.Is(err, ExitError(1)) {
			t.Errorf("err = %v, want ExitError(1)", err)
		}
		if !bytes.Contains(buf.Bytes(), []byte("FAIL")) {
			t.Errorf("report = %q, want a FAIL line", buf.String())
		}
	})
}

// TestDoctorStatusSmoke runs the default doctor output end to end.
func TestDoctorStatusSmoke(t *testing.T) {
	var buf bytes.Buffer
	if err := runDoctorStatus(&buf); err != nil {
		t.Fatalf("doctor status: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"hard-block", "claude-code", "hook entry", "L0"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

// TestDoctorBenchRealRun exercises the bench through a real built binary.
func TestDoctorBenchRealRun(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skips the spawn bench")
	}
	bin := buildGlove(t)
	samples, err := benchHook(bin, 5)
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	if len(samples) != 5 {
		t.Fatalf("got %d samples, want 5", len(samples))
	}
}

// TestExitCodeContract pins the PRD §6 exit-code surface of the hook path
// beyond the AC pair.
func TestExitCodeContract(t *testing.T) {
	t.Run("review_exits_zero", func(t *testing.T) {
		var out bytes.Buffer
		code := RunHook(&out, io.Discard, "claude-code",
			strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"sudo ls"}}`))
		if code != 0 {
			t.Fatalf("review exit = %d, want 0", code)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"ask"`)) {
			t.Errorf("stdout = %s, want ask", out.String())
		}
	})
	t.Run("allow_exits_zero", func(t *testing.T) {
		var out bytes.Buffer
		code := RunHook(&out, io.Discard, "claude-code",
			strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git status"}}`))
		if code != 0 {
			t.Fatalf("allow exit = %d, want 0", code)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"allow"`)) {
			t.Errorf("stdout = %s, want allow", out.String())
		}
	})
}
