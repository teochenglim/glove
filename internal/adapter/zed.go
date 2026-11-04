package adapter

import (
	"context"
	"fmt"
	"os"

	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/pkg/contract"
)

// ZedName is the stable adapter identifier used in the CLI.
const ZedName = "zed"

// hookEntryZed is the audit/evaluation entry: `glove hook zed` accepts the
// generic tool-call JSON on stdin, evaluates it, writes the audit record,
// and prints the shim-neutral verdict JSON. Nothing invokes it today — see
// Mode audit below.
const hookEntryZed = "glove hook zed"

// Zed is audit-only by design: Zed's agent exposes no pre-tool hook surface
// (no hooks pages in zed.dev/docs as of 2026-11; the extension API covers
// languages/themes/MCP context servers only). Install writes NOTHING to
// Zed's config — it prints the honest limitation and points at
// docs/install/zed.md, which recommends the layered posture: Zed
// `agent.tool_permissions` for coarse pre-execution gating (declarative
// rules), glove for fine-grained evaluation and the immutable audit trail.
// Zed persists agent threads (tool calls included) to SQLite under its data
// dir — that is the documented future observation point for post-hoc
// ingestion; nothing reads it in this release.
type Zed struct{}

// NewZed returns the audit-only adapter.
func NewZed() *Zed { return &Zed{} }

func (a *Zed) Name() string { return ZedName }

func (a *Zed) HookEntry() string { return hookEntryZed }

func (a *Zed) Detect() (bool, string, error) {
	return detectConfigDir(paths.ZedSettingsPath(), "zed")
}

// Install is an intentional no-op: audit mode writes no config. The output
// is the "diff" — the honest statement that nothing changes and where to
// read more.
func (a *Zed) Install(ctx context.Context, opts InstallOptions) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "glove install zed → audit mode — no config change")
	fmt.Fprintln(w, "  Zed exposes no pre-tool hook surface, so glove cannot block before")
	fmt.Fprintln(w, "  execution there. Recommended posture (docs/install/zed.md):")
	fmt.Fprintln(w, "    1. Zed agent.tool_permissions — coarse pre-execution gating")
	fmt.Fprintln(w, "    2. glove audit — fine-grained evaluation and the immutable trail")
	fmt.Fprintln(w, "    3. `glove hook zed` evaluates any tool call on demand (audited)")
	return nil
}

// Uninstall is the mirror no-op: audit mode installed nothing.
func (a *Zed) Uninstall(ctx context.Context) error {
	fmt.Println("zed: audit mode — nothing to uninstall")
	return nil
}

func (a *Zed) Status() AgentStatus {
	st := AgentStatus{
		Name:       ZedName,
		Mode:       ModeAudit,
		ConfigPath: paths.ZedSettingsPath(),
		Entry:      hookEntryZed,
	}
	st.Detected, st.ConfigPath, _ = a.Detect()
	// HookInstalled stays false by definition: audit mode has no hook entry.
	// Healthy — audit mode is the designed posture for zed, not a defect.
	return st
}

func (a *Zed) RenderVerdict(v contract.Verdict) ([]byte, error) {
	return renderShimVerdict(v)
}

func (a *Zed) ParseHookInput(raw []byte) (HookInput, error) {
	return parseClaudeStyleInput(ZedName, raw)
}
