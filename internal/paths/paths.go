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

// KiloPluginPath is the glove plugin shim for Kilo
// (~/.config/kilo/plugin/glove.ts, PRD §11).
func KiloPluginPath() string {
	return homeJoin(".config", "kilo", "plugin", "glove.ts")
}

// OpenCodePluginPath is the glove plugin shim for OpenCode
// (~/.config/opencode/plugin/glove.ts, PRD §11).
func OpenCodePluginPath() string {
	return homeJoin(".config", "opencode", "plugin", "glove.ts")
}

// CodexConfigPath is the Codex CLI config file (~/.codex/config.toml).
func CodexConfigPath() string {
	return homeJoin(".codex", "config.toml")
}

// CodexHooksPath is the Codex CLI hooks file (~/.codex/hooks.json) — the
// Claude-Code-shaped hook config Codex merges with any [hooks] tables in
// config.toml.
func CodexHooksPath() string {
	return homeJoin(".codex", "hooks.json")
}

// CursorHooksPath is the Cursor user-level hooks configuration
// (~/.cursor/hooks.json). Cursor's MCP config (~/.cursor/mcp.json) is a
// different file glove does not manage.
func CursorHooksPath() string {
	return homeJoin(".cursor", "hooks.json")
}

// GeminiSettingsPath is the Gemini CLI user settings (~/.gemini/settings.json).
func GeminiSettingsPath() string {
	return homeJoin(".gemini", "settings.json")
}

// ZedSettingsPath is the Zed user settings (~/.config/zed/settings.json on
// Linux, ~/.zed/settings.json on macOS).
func ZedSettingsPath() string {
	if runtime.GOOS == "darwin" {
		return homeJoin(".zed", "settings.json")
	}
	return homeJoin(".config", "zed", "settings.json")
}

func homeJoin(elem ...string) string {
	h := HomeDir()
	if h == "" {
		return ""
	}
	return filepath.Join(append([]string{h}, elem...)...)
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
