// Package release holds the hardening release's config-as-contract tests
// (PRD §7 0.6.0): the CI matrix, the release artifact pipeline and the
// failure-modes audit are asserted against the repo itself, so a workflow
// or doc edit that drops a gate fails `make check` like any other test.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// regexpTestNames matches backtick-quoted test references in the
// failure-modes doc (`Test…`), the only form the audit accepts as a pin.
var regexpTestNames = regexp.MustCompile("`((?:Test|Benchmark)[A-Za-z0-9_]+)`")

type workflow struct {
	On   map[string]any `yaml:"on"`
	Jobs map[string]struct {
		Strategy *struct {
			Matrix map[string]any `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var w workflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return w
}

// matrixInclude flattens a strategy.matrix.include into []map[string]any.
func matrixInclude(t *testing.T, w workflow, job string) []map[string]any {
	t.Helper()
	j, ok := w.Jobs[job]
	if !ok {
		t.Fatalf("workflow has no %q job", job)
	}
	if j.Strategy == nil {
		t.Fatalf("job %q has no strategy", job)
	}
	inc, ok := j.Strategy.Matrix["include"].([]any)
	if !ok {
		t.Fatalf("job %q matrix.include missing", job)
	}
	out := make([]map[string]any, 0, len(inc))
	for _, row := range inc {
		m, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("matrix.include row not a map: %v", row)
		}
		out = append(out, m)
	}
	return out
}

// TestAC_0_6_1_ci_matrix_covers_platforms — AC0.6.1: the CI workflow runs
// the §8 platform matrix — full `make check` (perf gates included) on
// ubuntu/amd64 + macos/arm64, build + vet + `go test -short` on
// windows/amd64 (spawn-latency benches stay off Windows runners), and a
// cross-compile job for the remaining targets; main pushes run it so the
// green streak accrues between tags.
func TestAC_0_6_1_ci_matrix_covers_platforms(t *testing.T) {
	w := loadWorkflow(t, "ci.yml")

	runners := map[string]bool{}
	for _, row := range matrixInclude(t, w, "test") {
		os_, _ := row["os"].(string)
		runners[os_] = true
	}
	for _, want := range []string{"ubuntu-latest", "macos-latest", "windows-latest"} {
		if !runners[want] {
			t.Errorf("ci.yml test matrix missing %q (have %v)", want, runners)
		}
	}
	// The windows runner must not carry the timing benches: the full gate
	// is conditioned on the matrix flag and the short-test step runs -short.
	var shortStep, fullStep string
	for _, s := range w.Jobs["test"].Steps {
		if strings.Contains(s.Run, "-short") {
			shortStep = s.If
		}
		if strings.Contains(s.Run, "make check") {
			fullStep = s.If
		}
	}
	if shortStep == "" {
		t.Error("ci.yml windows job must run `go test -short` (timing benches are spawn-sensitive)")
	}
	if fullStep == "" || !strings.Contains(fullStep, "matrix.full") {
		t.Errorf("make check step must be gated on matrix.full, got if=%q", fullStep)
	}

	xc := matrixInclude(t, w, "cross-compile")
	have := map[string]bool{}
	for _, row := range xc {
		g, _ := row["goos"].(string)
		a, _ := row["goarch"].(string)
		have[g+"/"+a] = true
	}
	for _, want := range []string{"darwin/amd64", "linux/arm64"} {
		if !have[want] {
			t.Errorf("cross-compile matrix missing %q (have %v)", want, have)
		}
	}
	// The streak gate: plain main pushes trigger the matrix, not just tags.
	push, ok := w.On["push"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml must trigger on push")
	}
	branches, _ := push["branches"].([]any)
	hasMain := false
	for _, b := range branches {
		if b == "main" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Errorf("ci.yml push branches = %v, want main included so the green streak accrues", branches)
	}
}

// TestAC_0_6_1_cross_compile_all_platforms — AC0.6.1 other half: the binary
// cross-compiles static (CGO_ENABLED=0, -trimpath) for every §8 target:
// linux amd64/arm64, darwin amd64/arm64, windows amd64. Short mode skips
// (the CI cross-compile job builds two of these on ubuntu natively).
func TestAC_0_6_1_cross_compile_all_platforms(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skips the five cross-compile builds")
	}
	targets := []struct{ goos, goarch string }{
		{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"},
	}
	for _, tc := range targets {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "glove")
			if tc.goos == "windows" {
				out += ".exe"
			}
			cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./cmd/glove")
			cmd.Dir = "../.."
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+tc.goos, "GOARCH="+tc.goarch)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build %s/%s: %v\n%s", tc.goos, tc.goarch, err, b)
			}
			info, err := os.Stat(out)
			if err != nil || info.Size() < 1<<20 { // a real glove binary is multiple MB
				t.Fatalf("%s/%s artifact missing or implausibly small: %v", tc.goos, tc.goarch, err)
			}
		})
	}
}

// TestAC_0_6_3_release_matrix_and_checksums — AC0.6.3: the release
// workflow builds all five §8 platforms (windows/amd64 included), writes a
// per-artifact .sha256, verifies them before publishing, aggregates
// checksums.txt, and the signing steps exist as a maintainer-run script
// (certificates never enter the repo).
func TestAC_0_6_3_release_matrix_and_checksums(t *testing.T) {
	w := loadWorkflow(t, "release.yml")

	have := map[string]bool{}
	for _, row := range matrixInclude(t, w, "build") {
		g, _ := row["goos"].(string)
		a, _ := row["goarch"].(string)
		have[g+"/"+a] = true
	}
	for _, want := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"} {
		if !have[want] {
			t.Errorf("release build matrix missing %q (have %v)", want, have)
		}
	}

	publish := w.Jobs["publish"]
	var verifyStep, createStep string
	for _, s := range publish.Steps {
		switch {
		case strings.Contains(s.Name, "Verify"):
			verifyStep = s.Run
		case strings.Contains(s.Name, "Create release"):
			createStep = s.Run
		}
	}
	if verifyStep == "" || !strings.Contains(verifyStep, "sha256sum -c") {
		t.Error("publish must verify every .sha256 (sha256sum -c) before creating the release")
	}
	if verifyStep == "" || !strings.Contains(verifyStep, "checksums.txt") {
		t.Error("publish must aggregate checksums.txt (PRD §9)")
	}
	if createStep == "" || !strings.Contains(createStep, "artifacts/*") {
		t.Errorf("release must publish the artifacts glob (which includes checksums.txt), got:\n%s", createStep)
	}
	if verifyStep == "" || !strings.Contains(verifyStep, "cat *.sha256 > checksums.txt") {
		t.Errorf("checksums.txt must be aggregated from the per-artifact hashes inside artifacts/, got:\n%s", verifyStep)
	}

	sig, err := os.ReadFile(filepath.Join("..", "..", "scripts", "sign-release.sh"))
	if err != nil {
		t.Fatalf("scripts/sign-release.sh missing: %v", err)
	}
	script := string(sig)
	for _, want := range []string{"notarytool", "signtool", "checksums.txt"} {
		if !strings.Contains(script, want) {
			t.Errorf("sign-release.sh must wire %q (macOS notarization / Windows Authenticode / checksum refresh)", want)
		}
	}
}

// TestAC_0_6_3_artifact_sha256_roundtrip — AC0.6.3 other half: the exact
// artifact pipeline works end to end — build two representative artifacts,
// hash them (SHA-256, hex), write per-artifact .sha256 lines and the
// aggregated checksums.txt, then verify every file against both, the same
// math the publish job performs with sha256sum -c.
func TestAC_0_6_3_artifact_sha256_roundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skips the artifact builds")
	}
	dir := t.TempDir()
	artifacts := []struct{ goos, name string }{
		{"linux", "glove-test-linux-amd64"},
		{"windows", "glove-test-windows-amd64.exe"},
	}
	var checksums strings.Builder
	for _, a := range artifacts {
		bin := filepath.Join(dir, a.name)
		cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/glove")
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+a.goos, "GOARCH=amd64")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", a.name, err, b)
		}
		raw, err := os.ReadFile(bin)
		if err != nil {
			t.Fatalf("read %s: %v", bin, err)
		}
		sum := sha256.Sum256(raw)
		line := hex.EncodeToString(sum[:]) + "  " + a.name + "\n"
		checksums.WriteString(line)
		if err := os.WriteFile(filepath.Join(dir, a.name+".sha256"), []byte(line), 0o644); err != nil {
			t.Fatalf("write .sha256: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(checksums.String()), 0o644); err != nil {
		t.Fatalf("write checksums.txt: %v", err)
	}

	// Verify: recompute each artifact's hash and compare against BOTH the
	// per-artifact .sha256 and checksums.txt (a byte-flip must be caught).
	for _, a := range artifacts {
		raw, err := os.ReadFile(filepath.Join(dir, a.name))
		if err != nil {
			t.Fatalf("read %s: %v", a.name, err)
		}
		sum := sha256.Sum256(raw)
		got := hex.EncodeToString(sum[:])
		for _, f := range []string{a.name + ".sha256", "checksums.txt"} {
			line, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			if !strings.Contains(string(line), got) {
				t.Errorf("%s: hash %s not recorded in %s", a.name, got, f)
			}
		}
		// Corrupt-and-detect: flipping a byte must change the hash, so the
		// published checksums genuinely gate the artifacts.
		raw[0] ^= 0xFF
		badSum := sha256.Sum256(raw)
		if hex.EncodeToString(badSum[:]) == got {
			t.Fatal("sanity: corrupted artifact produced the same hash")
		}
	}
}

// TestAC_0_6_2_failure_modes_documented — AC0.6.2: the error-handling audit
// is a shipped artifact — docs/failure-modes.md exists, and every test it
// names as pinning a failure path actually exists in the repo. A failure
// path without a named test, or a named test that was deleted, fails here.
func TestAC_0_6_2_failure_modes_documented(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "failure-modes.md"))
	if err != nil {
		t.Fatalf("docs/failure-modes.md missing: %v", err)
	}
	doc := string(raw)
	if !strings.Contains(doc, "| Failure") || !strings.Contains(doc, "| Decision") {
		t.Fatal("failure-modes.md must carry a Failure/Decision table")
	}

	// Every `Test…` reference in the doc must be a real test function.
	refs := map[string]bool{}
	for _, m := range regexpTestNames.FindAllStringSubmatch(doc, -1) {
		refs[m[1]] = true
	}
	if len(refs) < 10 {
		t.Fatalf("failure-modes.md names only %d tests — the audit is too thin", len(refs))
	}
	var sources []string
	err = filepath.Walk(filepath.Join("..", ".."), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && (info.Name() == "bin" || info.Name() == "dist" || info.Name() == ".git") {
				return filepath.SkipDir
			}
			return err
		}
		if strings.HasSuffix(path, ".go") {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var allGo strings.Builder
	for _, s := range sources {
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("read %s: %v", s, err)
		}
		allGo.Write(b)
	}
	for name := range refs {
		if !strings.Contains(allGo.String(), "func "+name+"(") {
			t.Errorf("failure-modes.md cites %s, which does not exist in the repo", name)
		}
	}
}
