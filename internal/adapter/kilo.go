package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// KiloName is the stable adapter identifier used in the CLI.
const KiloName = "kilo"

// hookEntryKilo is the hook entry the generated shim invokes. The shim
// itself is the hook surface (tool.execute.before, PRD §11); this entry is
// what `glove list`/doctor display and what the shim spawns per call.
const hookEntryKilo = "glove hook kilo"

// Kilo adapts Kilo (the OpenCode-fork CLI): a TypeScript plugin shim at
// ~/.config/kilo/plugin/glove.ts intercepts tool.execute.before, spawns
// `glove hook kilo`, and throws on block/review. Structurally different
// from every settings-file adapter — it proves the Adapter abstraction
// holds beyond JSON configs (PRD §7 0.5.0, sequenced first).
type Kilo struct {
	pluginPath string
}

// NewKilo returns the adapter for the default plugin path.
func NewKilo() *Kilo { return &Kilo{pluginPath: paths.KiloPluginPath()} }

// NewKiloAt returns the adapter pinned to a specific plugin file (tests).
func NewKiloAt(path string) *Kilo { return &Kilo{pluginPath: path} }

func (a *Kilo) Name() string { return KiloName }

func (a *Kilo) HookEntry() string { return hookEntryKilo }

func (a *Kilo) Detect() (bool, string, error) {
	if installed, path, err := detectShim(a.pluginPath); installed || err != nil {
		return installed, path, err
	}
	// The plugin dir may not exist yet while the CLI is installed.
	if p, err := exec.LookPath("kilo"); err == nil && p != "" {
		return true, a.pluginPath, nil
	}
	return false, a.pluginPath, nil
}

func (a *Kilo) Install(ctx context.Context, opts InstallOptions) error {
	data, err := shimHeaderFor(KiloName)
	if err != nil {
		return err
	}
	content, err := renderShim(data)
	if err != nil {
		return err
	}
	return installShim(ctx, a.pluginPath, content, KiloName, opts)
}

// Uninstall removes the generated shim. Idempotent: nothing installed →
// no-op. Refuses to delete a file glove did not generate.
func (a *Kilo) Uninstall(ctx context.Context) error {
	return uninstallShim(ctx, a.pluginPath, os.Stdout)
}

func (a *Kilo) Status() AgentStatus {
	return shimStatus(a.pluginPath, KiloName, hookEntryKilo)
}

func (a *Kilo) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderShimVerdict(v)
}

func (a *Kilo) ParseHookInput(raw []byte) (HookInput, error) {
	return parseShimInput(KiloName, raw)
}
