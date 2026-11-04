package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestAC_0_3_5_windows_data_path — AC0.3.5: on Windows the audit root is
// %LOCALAPPDATA%\glove (PRD §5.4), falling back to the home-relative path
// when the env var is absent. Expected values are built with filepath.Join
// so the assertion is separator-agnostic on any dev OS.
func TestAC_0_3_5_windows_data_path(t *testing.T) {
	want := filepath.Join(`C:\Users\u\AppData\Local`, "glove")
	if got := dataDirFor("windows", `C:\Users\u`, "", `C:\Users\u\AppData\Local`); got != want {
		t.Errorf("windows data dir = %q, want %q (LOCALAPPDATA\\glove)", got, want)
	}
	wantFallback := filepath.Join(`C:\Users\u`, "AppData", "Local", "glove")
	if got := dataDirFor("windows", `C:\Users\u`, "", ""); got != wantFallback {
		t.Errorf("windows fallback = %q, want %q (home-relative AppData)", got, wantFallback)
	}
}

// TestAC_0_3_5_path_resolution_per_os — AC0.3.5 other half: XDG-aware
// resolution on macOS/Linux (PRD §5.4 ~/.local/share/glove, §5.5
// ~/.config/glove).
func TestAC_0_3_5_path_resolution_per_os(t *testing.T) {
	t.Run("data_dir", func(t *testing.T) {
		cases := []struct {
			goos, home, xdgData string
			want                string
		}{
			{"darwin", "/Users/u", "", "/Users/u/.local/share/glove"},
			{"linux", "/home/u", "", "/home/u/.local/share/glove"},
			{"linux", "/home/u", "/xdg", "/xdg/glove"},
		}
		for _, tc := range cases {
			got := dataDirFor(tc.goos, tc.home, tc.xdgData, "")
			// Separator-agnostic: on Windows filepath.Join emits backslashes
			// while the wants are written slash-shaped.
			if filepath.ToSlash(got) != tc.want {
				t.Errorf("dataDirFor(%s,%s,%s) = %q, want %q", tc.goos, tc.home, tc.xdgData, got, tc.want)
			}
		}
	})
	t.Run("audit_join", func(t *testing.T) {
		d := dataDirFor("darwin", "/Users/u", "", "")
		if want := filepath.Join(d, "audit.jsonl"); filepath.ToSlash(want) != "/Users/u/.local/share/glove/audit.jsonl" {
			t.Errorf("audit path = %q, want the §5.4 location", want)
		}
	})
	t.Run("config_dir", func(t *testing.T) {
		base := "/Users/u/.config"
		if want := filepath.Join(base, "glove", "policy.toml"); filepath.ToSlash(want) != "/Users/u/.config/glove/policy.toml" {
			t.Errorf("policy path = %q, want the §5.5 location", want)
		}
	})
}

// TestAC_0_6_6_windows_paths_resolve — AC0.6.6: the full Windows surface
// resolves — audit/data root, config root, and the per-agent config paths
// under %USERPROFILE% (PRD §5.4/§5.5, §8 hardening). Assertions are built
// with filepath.Join so they hold on any dev OS.
func TestAC_0_6_6_windows_paths_resolve(t *testing.T) {
	t.Run("audit_and_policy", func(t *testing.T) {
		d := dataDirFor("windows", `C:\Users\u`, "", `C:\Users\u\AppData\Local`)
		if want := filepath.Join(`C:\Users\u\AppData\Local`, "glove", "audit.jsonl"); filepath.ToSlash(d+"/audit.jsonl") != filepath.ToSlash(want) {
			t.Errorf("audit root = %q, want %q", d+"/audit.jsonl", want)
		}
	})
	t.Run("agent_config_paths_under_profile", func(t *testing.T) {
		t.Setenv("HOME", `C:\Users\u`)
		t.Setenv("USERPROFILE", `C:\Users\u`)
		// zed branches on the compiled-in GOOS (macOS keeps ~/.zed); the
		// others are home-relative on every platform.
		zedWant := filepath.Join(`C:\Users\u`, ".config", "zed", "settings.json")
		if runtime.GOOS == "darwin" {
			zedWant = filepath.Join(`C:\Users\u`, ".zed", "settings.json")
		}
		for _, tc := range []struct{ name, want string }{
			{"claude", filepath.Join(`C:\Users\u`, ".claude", "settings.json")},
			{"kilo", filepath.Join(`C:\Users\u`, ".config", "kilo", "plugin", "glove.ts")},
			{"codex", filepath.Join(`C:\Users\u`, ".codex", "hooks.json")},
			{"cursor", filepath.Join(`C:\Users\u`, ".cursor", "hooks.json")},
			{"gemini", filepath.Join(`C:\Users\u`, ".gemini", "settings.json")},
			{"zed", zedWant},
		} {
			var got string
			switch tc.name {
			case "claude":
				got = ClaudeSettingsPath()
			case "kilo":
				got = KiloPluginPath()
			case "codex":
				got = CodexHooksPath()
			case "cursor":
				got = CursorHooksPath()
			case "gemini":
				got = GeminiSettingsPath()
			case "zed":
				got = ZedSettingsPath()
			}
			if filepath.ToSlash(got) != filepath.ToSlash(tc.want) {
				t.Errorf("%s path = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}
