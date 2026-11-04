// Package paths resolves OS-specific configuration and data locations
// (PRD §5.4, §5.5). Overrides exist for tests and sandboxes: GLOVE_DATA_DIR
// and GLOVE_CONFIG_DIR replace the derived roots wholesale.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// HomeDir returns the user's home directory, or "" when it cannot be
// determined. Callers treat "" as "no home context" rather than guessing.
func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// ClaudeSettingsPath is the user-level Claude Code settings file
// (~/.claude/settings.json). Project-level (.claude/settings.json) support
// arrives with deeper policy integration.
func ClaudeSettingsPath() string {
	h := HomeDir()
	if h == "" {
		return ""
	}
	return filepath.Join(h, ".claude", "settings.json")
}

// DataDir is glove's state root: ~/.local/share/glove on macOS/Linux
// (XDG_DATA_HOME honoured), %LOCALAPPDATA%\glove on Windows (PRD §5.4).
func DataDir() string {
	if v := os.Getenv("GLOVE_DATA_DIR"); v != "" {
		return v
	}
	return dataDirFor(runtime.GOOS, HomeDir(), os.Getenv("XDG_DATA_HOME"), os.Getenv("LOCALAPPDATA"))
}

func dataDirFor(goos, home, xdgData, localAppData string) string {
	switch goos {
	case "windows":
		if localAppData != "" {
			return filepath.Join(localAppData, "glove")
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Local", "glove")
		}
		return ""
	default:
		base := xdgData
		if base == "" && home != "" {
			base = filepath.Join(home, ".local", "share")
		}
		if base == "" {
			return ""
		}
		return filepath.Join(base, "glove")
	}
}

// AuditPath is the audit JSONL file (PRD §5.4).
func AuditPath() string {
	d := DataDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "audit.jsonl")
}

// ConfigDir is glove's config root for the user-level policy file:
// ~/.config/glove on macOS and Linux alike (XDG_CONFIG_HOME honoured),
// %LOCALAPPDATA%\glove on Windows (PRD §5.5). GLOVE_CONFIG_DIR overrides.
func ConfigDir() string {
	if v := os.Getenv("GLOVE_CONFIG_DIR"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "glove")
		}
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if h := HomeDir(); h != "" {
			base = filepath.Join(h, ".config")
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "glove")
}

// PolicyUserPath is the user-level policy file (~/.config/glove/policy.toml).
func PolicyUserPath() string {
	d := ConfigDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "policy.toml")
}
