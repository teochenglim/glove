package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestAC_0_3_9_project_beats_user — AC0.3.9: a scalar set in both the user
// and the project policy resolves to the project's value (explicit PRD §7
// test), and list-valued keys concatenate across layers.
func TestAC_0_3_9_project_beats_user(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	t.Setenv("GLOVE_CONFIG_DIR", cfg)
	writeFile(t, filepath.Join(cfg, "policy.toml"), `
[general]
mode = "fast"
on_model_unavailable = "block"

[paths]
protected = ["**/user-secrets/**"]
`)
	writeFile(t, filepath.Join(project, ".glove.toml"), `
[general]
mode = "offline"

[paths]
protected = ["**/project-secrets/**"]
`)

	t.Chdir(project)
	p, src, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.General.Mode != ModeOffline {
		t.Errorf("mode = %q, want offline (project overrides user)", p.General.Mode)
	}
	// on_model_unavailable is set only at user level — it survives.
	if p.General.OnModelUnavailable != "block" {
		t.Errorf("on_model_unavailable = %q, want block (user value kept)", p.General.OnModelUnavailable)
	}
	// Lists concatenate: both layers' patterns are enforced.
	if len(p.Paths.Protected) != 2 {
		t.Errorf("protected = %v, want both user and project patterns", p.Paths.Protected)
	}
	if src.User == "" || src.Project == "" {
		t.Errorf("sources = %+v, want both files recorded", src)
	}
}

// TestAC_0_3_9_walk_up_discovery — AC0.3.9 other half: .glove.toml is found
// from a nested working directory by walking up.
func TestAC_0_3_9_walk_up_discovery(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	t.Setenv("GLOVE_CONFIG_DIR", cfg)
	writeFile(t, filepath.Join(project, ".glove.toml"), `
[general]
mode = "fast"
`)
	deep := filepath.Join(project, "sub", "deeper")
	writeFile(t, filepath.Join(deep, ".gitkeep"), "")

	t.Chdir(deep)
	p, src, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.General.Mode != ModeFast {
		t.Errorf("mode = %q, want fast from the walked-up project policy", p.General.Mode)
	}
	if src.Project == "" {
		t.Error("project source not recorded for the walked-up policy")
	}
}

// TestAC_0_3_7_validate_reports_line — AC0.3.7: TOML syntax errors carry
// the parser's line number; schema errors name the offending key path.
func TestAC_0_3_7_validate_reports_line(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.toml")
	writeFile(t, path, "[general]\nmode = \n") // unterminated value on line 2

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("broken TOML must fail validation")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error = %v, want a line number from the parser", err)
	}

	sem := filepath.Join(t.TempDir(), "semantic.toml")
	writeFile(t, sem, `
[general]
mode = "turbo"

[rules]
hard_block = ["((("]
`)
	_, err = LoadFile(sem)
	if err == nil {
		t.Fatal("invalid enum and regexp must fail validation")
	}
	for _, want := range []string{"general.mode", "rules.hard_block[0]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want the key path %q", err, want)
		}
	}
}

// TestDefaultsValid — the shipped defaults must always pass validation.
func TestDefaultsValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

// TestHashChangesWhenPolicyChanges — the reload detector's contract.
func TestHashChangesWhenPolicyChanges(t *testing.T) {
	a := Defaults()
	b := Defaults()
	h1, err := a.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b.Rules.HardBlock = []string{"\\bcurl\\b"}
	h2, err := b.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if h1 == h2 {
		t.Error("hash did not change when the policy changed")
	}
}
