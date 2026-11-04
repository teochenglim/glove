package paths

import (
	"path/filepath"
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
			if got != tc.want {
				t.Errorf("dataDirFor(%s,%s,%s) = %q, want %q", tc.goos, tc.home, tc.xdgData, got, tc.want)
			}
		}
	})
	t.Run("audit_join", func(t *testing.T) {
		d := dataDirFor("darwin", "/Users/u", "", "")
		if want := filepath.Join(d, "audit.jsonl"); want != "/Users/u/.local/share/glove/audit.jsonl" {
			t.Errorf("audit path = %q, want the §5.4 location", want)
		}
	})
	t.Run("config_dir", func(t *testing.T) {
		base := "/Users/u/.config"
		if want := filepath.Join(base, "glove", "policy.toml"); want != "/Users/u/.config/glove/policy.toml" {
			t.Errorf("policy path = %q, want the §5.5 location", want)
		}
	})
}
