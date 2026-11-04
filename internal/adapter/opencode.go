package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// OpenCodeName is the stable adapter identifier used in the CLI.
const OpenCodeName = "opencode"

// hookEntryOpenCode is the hook entry the generated shim invokes.
const hookEntryOpenCode = "glove hook opencode"

// OpenCode adapts OpenCode: a TypeScript plugin shim at
// ~/.config/opencode/plugin/glove.ts intercepts tool.execute.before —
// Kilo's lineage (Kilo is a fork), so both share the shim machinery and
// differ only in paths, import and export shape (PRD §7 0.5.0).
//
// The shim targets the stable V1 plugin API (@opencode-ai/plugin 1.x).
// OpenCode's V2 beta replaces the plugin API ("V1 plugin implementations do
// not run in V2", per its migration doc) — when V2 ships stable, this file
// and the template are the only things to touch; docs/install/opencode.md
// tracks the caveat.
type OpenCode struct {
	pluginPath string
}

// NewOpenCode returns the adapter for the default plugin path.
func NewOpenCode() *OpenCode { return &OpenCode{pluginPath: paths.OpenCodePluginPath()} }

// NewOpenCodeAt returns the adapter pinned to a specific plugin file (tests).
func NewOpenCodeAt(path string) *OpenCode { return &OpenCode{pluginPath: path} }

func (a *OpenCode) Name() string { return OpenCodeName }

func (a *OpenCode) HookEntry() string { return hookEntryOpenCode }

func (a *OpenCode) Detect() (bool, string, error) {
	if installed, path, err := detectShim(a.pluginPath); installed || err != nil {
		return installed, path, err
	}
	if p, err := exec.LookPath("opencode"); err == nil && p != "" {
		return true, a.pluginPath, nil
	}
	return false, a.pluginPath, nil
}

func (a *OpenCode) Install(ctx context.Context, opts InstallOptions) error {
	data, err := shimHeaderFor(OpenCodeName)
	if err != nil {
		return err
	}
	content, err := renderShim(data)
	if err != nil {
		return err
	}
	return installShim(ctx, a.pluginPath, content, OpenCodeName, opts)
}

func (a *OpenCode) Uninstall(ctx context.Context) error {
	return uninstallShim(ctx, a.pluginPath, os.Stdout)
}

func (a *OpenCode) Status() AgentStatus {
	return shimStatus(a.pluginPath, OpenCodeName, hookEntryOpenCode)
}

func (a *OpenCode) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderShimVerdict(v)
}

func (a *OpenCode) ParseHookInput(raw []byte) (HookInput, error) {
	return parseShimInput(OpenCodeName, raw)
}
