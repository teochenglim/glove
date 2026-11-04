// Package paths resolves OS-specific configuration and data locations
// (PRD §5.4). Glove's own config/data dirs arrive with the audit release
// (v0.3.0); v0.1.0 only needs the agent config locations.
package paths

import (
	"os"
	"path/filepath"
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
// arrives with the policy engine (v0.7.0).
func ClaudeSettingsPath() string {
	h := HomeDir()
	if h == "" {
		return ""
	}
	return filepath.Join(h, ".claude", "settings.json")
}
