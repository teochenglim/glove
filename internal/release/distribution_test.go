package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseMatrix reads the build job's goos/goarch matrix from release.yml —
// the packaging artifacts must match it exactly.
func releaseMatrix(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						Goos   string `yaml:"goos"`
						Goarch string `yaml:"goarch"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	for _, job := range wf.Jobs {
		if len(job.Strategy.Matrix.Include) == 0 {
			continue
		}
		var out []string
		for _, inc := range job.Strategy.Matrix.Include {
			out = append(out, inc.Goos+"-"+inc.Goarch)
		}
		return out
	}
	t.Fatal("no build matrix found in release.yml")
	return nil
}

func currentVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// TestAC_0_9_1_formula_matches_release_layout — AC0.9.1: the Homebrew
// formula's four download URLs match the release workflow's artifact naming
// exactly, each carries a sha256 line, the version tracks VERSION, and the
// install blocks map the release binary onto `glove`.
func TestAC_0_9_1_formula_matches_release_layout(t *testing.T) {
	platforms := releaseMatrix(t)
	if len(platforms) != 4 {
		t.Fatalf("release matrix = %v, want the four shipped platforms", platforms)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "packaging", "homebrew", "glove.rb"))
	if err != nil {
		t.Fatalf("formula missing: %v", err)
	}
	f := string(raw)

	// Every matrix platform has a URL line in the formula's exact naming.
	urlRe := regexp.MustCompile(`glove-v#\{version\}-([a-z]+-[a-z0-9]+)\.tar\.gz`)
	formulaPlatforms := map[string]bool{}
	for _, m := range urlRe.FindAllStringSubmatch(f, -1) {
		formulaPlatforms[m[1]] = true
	}
	for _, p := range platforms {
		if !formulaPlatforms[p] {
			t.Errorf("formula has no download URL for release platform %s", p)
		}
	}
	if len(formulaPlatforms) != len(platforms) {
		t.Errorf("formula covers %v, release matrix is %v — drift", formulaPlatforms, platforms)
	}

	// Each URL is followed by a sha256 line (placeholder until the release
	// fills it via scripts/update-homebrew-sha.sh — the shape is the point).
	lines := strings.Split(f, "\n")
	shaRe := regexp.MustCompile(`^\s*sha256 "[0-9a-f]{64}"`)
	for i, line := range lines {
		if urlRe.MatchString(line) {
			found := false
			for j := i + 1; j < len(lines) && j <= i+2; j++ {
				if shaRe.MatchString(lines[j]) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("URL %q has no sha256 line directly after it", strings.TrimSpace(line))
			}
		}
	}

	// The formula version must track releases: at most ONE minor behind
	// VERSION. It cannot be required to equal VERSION at CI time — the
	// sha256 fill (and often the version bump itself) happen around the tag
	// (v0.10.1's CI lesson: a strict >= check is a per-release catch-22).
	// Two or more minors behind means the formula stopped being maintained.
	ver := currentVersion(t)
	mv := regexp.MustCompile(`version "([0-9]+\.[0-9]+\.[0-9]+)"`).FindStringSubmatch(f)
	if mv == nil {
		t.Error("formula has no version line")
	} else if formulaMoreThanOneMinorBehind(mv[1], ver) {
		t.Errorf("formula version %s is more than one minor behind VERSION (%s) — bump packaging/homebrew/glove.rb with the release", mv[1], ver)
	}
	if !strings.Contains(f, `=> "glove"`) {
		t.Error("formula does not install the binary as glove")
	}
	if !strings.Contains(f, `license "Apache-2.0"`) {
		t.Error("formula license is not Apache-2.0")
	}
}

// TestAC_0_9_2_failure_modes_cover_new_paths — AC0.9.2: the failure-modes
// audit covers the 0.7.0 sink/verify and 0.8.0 correlation/bundle failure
// paths in their own sections, with rows and cited tests. Cited-test
// existence is enforced for the whole doc by
// TestAC_0_6_2_failure_modes_documented (config-as-contract).
func TestAC_0_9_2_failure_modes_cover_new_paths(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "failure-modes.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	sections := map[string]struct {
		mustMention []string
		minRows     int
	}{
		"Audit forwarding + tamper evidence (v0.7.0, ADR 4)": {
			mustMention: []string{"sink_off_by_default", "sink_failure_never_affects_verdict", "sink_config_validate", "verify_detects_edits", "chain_concurrent_and_rotation", "TestAC_0_3_7_invalid_policy_fails_loudly"},
			minRows:     9,
		},
		"Correlation + policy bundles (v0.8.0)": {
			mustMention: []string{"escalation_tightens_undecidable", "correlation_never_loosens", "import_rejects_tampered_bundle", "import_emits_policy_event"},
			minRows:     6,
		},
	}
	for header, want := range sections {
		idx := strings.Index(doc, header)
		if idx < 0 {
			t.Fatalf("failure-modes doc is missing the %q section", header)
		}
		next := len(doc)
		for h := range sections {
			if h != header {
				if i := strings.Index(doc, h); i > idx && i < next {
					next = i
				}
			}
		}
		body := doc[idx:next]
		rows := strings.Count(body, "\n| ")
		if rows < want.minRows {
			t.Errorf("%q section has %d rows, want >= %d", header, rows, want.minRows)
		}
		for _, m := range want.mustMention {
			if !strings.Contains(body, m) {
				t.Errorf("%q section does not cite %s", header, m)
			}
		}
	}
}

// TestAC_0_9_3_changelog_consolidated — AC0.9.3: CHANGELOG.md is the
// verbatim consolidation of RELEASE.md §4 — every version heading present,
// same newest-first order, same paragraph bodies. Drift fails make check.
func TestAC_0_9_3_changelog_consolidated(t *testing.T) {
	rel, err := os.ReadFile(filepath.Join("..", "..", "RELEASE.md"))
	if err != nil {
		t.Fatal(err)
	}
	ch, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("CHANGELOG.md missing: %v", err)
	}
	sectionsOf := func(src string) []string {
		var out []string
		var cur strings.Builder
		in := false
		for _, line := range strings.Split(src, "\n") {
			if strings.HasPrefix(line, "### v") {
				if in {
					out = append(out, strings.TrimRight(cur.String(), "\n"))
					cur.Reset()
				}
				in = true
			} else if in && strings.HasPrefix(line, "## ") {
				// A level-2 heading ends the section (RELEASE.md §4 ends at §5).
				out = append(out, strings.TrimRight(cur.String(), "\n"))
				cur.Reset()
				in = false
				continue
			}
			if in {
				cur.WriteString(line + "\n")
			}
		}
		if in {
			out = append(out, strings.TrimRight(cur.String(), "\n"))
		}
		return out
	}
	want := sectionsOf(string(rel))
	got := sectionsOf(string(ch))
	if len(got) != len(want) {
		t.Fatalf("CHANGELOG has %d version sections, RELEASE.md §4 has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CHANGELOG section %d drifted from RELEASE.md §4:\n--- changelog ---\n%s\n--- release ---\n%s", i+1, got[i], want[i])
		}
	}
}

// formulaMoreThanOneMinorBehind reports whether the formula's dotted semver
// lags VERSION by more than one minor (patch level ignored — the tap copy
// is refreshed per release; only sustained neglect fails the gate).
func formulaMoreThanOneMinorBehind(formula, ver string) bool {
	fm := strings.Split(formula, ".")
	vm := strings.Split(ver, ".")
	fMaj, fMin := atoiOr0(fm, 0), atoiOr0(fm, 1)
	vMaj, vMin := atoiOr0(vm, 0), atoiOr0(vm, 1)
	if fMaj != vMaj {
		return fMaj < vMaj
	}
	return vMin-fMin > 1
}

func atoiOr0(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n := 0
	for _, c := range parts[i] {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
