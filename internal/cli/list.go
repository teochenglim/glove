package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
)

// readmePath is the README `glove list --matrix --check` verifies
// (package var: tests pin it to a fixture).
var readmePath = "README.md"

// matrixBegin/matrixEnd bracket the machine-verified coverage table in
// README.md (PRD-0.5.0-6: no "to be verified" rows; drift fails CI).
const (
	matrixBegin = "<!-- coverage-matrix:begin -->"
	matrixEnd   = "<!-- coverage-matrix:end -->"
)

// matrixRow is one row of the agent coverage matrix — the single source of
// truth shared by `glove list --matrix` and README.md. Enforcement wording
// is deliberate: cursor and gemini render review as deny (their hook
// surfaces have no enforced ask), the only allowed direction.
type matrixRow struct {
	Agent       string
	Enforcement string
	Mechanism   string
	Install     string
}

func matrixRows() []matrixRow {
	return []matrixRow{
		{
			Agent:       "claude-code",
			Enforcement: "block / review / allow before execution",
			Mechanism:   "`PreToolUse` command hook → `glove hook claude-code`",
			Install:     "`glove install claude-code`",
		},
		{
			Agent:       "kilo",
			Enforcement: "block / review / allow before execution",
			Mechanism:   "plugin shim (`tool.execute.before`, throws on block/review) → `glove hook kilo`",
			Install:     "`glove install kilo`",
		},
		{
			Agent:       "codex",
			Enforcement: "block / review / allow before execution",
			Mechanism:   "`PreToolUse` command hook (Claude-compatible protocol) → `glove hook codex`",
			Install:     "`glove install codex`",
		},
		{
			Agent:       "opencode",
			Enforcement: "block / review / allow before execution",
			Mechanism:   "plugin shim (`tool.execute.before`, V1 plugin API) → `glove hook opencode`",
			Install:     "`glove install opencode`",
		},
		{
			Agent:       "cursor",
			Enforcement: "block / review (as deny — no enforced ask) / allow before execution",
			Mechanism:   "`preToolUse` command hook with `failClosed: true` → `glove hook cursor`",
			Install:     "`glove install cursor`",
		},
		{
			Agent:       "gemini",
			Enforcement: "block / review (as deny — no ask verb) / allow before execution",
			Mechanism:   "`BeforeTool` command hook (JSON deny + exit 2) → `glove hook gemini`",
			Install:     "`glove install gemini`",
		},
		{
			Agent:       "goose",
			Enforcement: "block / review (as deny — goose hooks are allow/block only) / allow before execution",
			Mechanism:   "blocking `PreToolUse` plugin hook with `on_failure: \"block\"` → `glove hook goose`",
			Install:     "`glove install goose`",
		},
		{
			Agent:       "zed",
			Enforcement: "audit only — Zed has no pre-tool hook surface",
			Mechanism:   "`glove hook zed` evaluates a tool call on demand + the audit log",
			Install:     "`glove install zed` (no config change)",
		},
	}
}

// renderMatrix emits the README table exactly as it must appear between the
// coverage-matrix markers.
func renderMatrix() string {
	var b strings.Builder
	b.WriteString("| Agent | Enforcement | Mechanism | Install |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range matrixRows() {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.Agent, r.Enforcement, r.Mechanism, r.Install)
	}
	return b.String()
}

func newListCmd() *cobra.Command {
	var matrix, check bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the agent matrix with install/hook health (PRD §6)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if matrix {
				return runMatrix(os.Stdout, check)
			}
			return runList(os.Stdout)
		},
	}
	cmd.Flags().BoolVar(&matrix, "matrix", false, "print the README coverage matrix")
	cmd.Flags().BoolVar(&check, "check", false, "with --matrix: verify README.md matches the generated matrix, exit 1 on drift")
	return cmd
}

// runList prints every registered agent with detection + hook health
// (PRD §7 0.5.0: "installed agents + hook health"; zed shows mode audit).
func runList(w io.Writer) error {
	fmt.Fprintf(w, "%-14s %-6s %-9s %s\n", "agent", "mode", "detected", "status")
	for _, name := range adapter.Names() {
		a, ok := adapter.For(name)
		if !ok {
			return fmt.Errorf("registered agent %q does not resolve", name)
		}
		st := adapter.StatusOf(a)
		status := listStatus(st, name)
		fmt.Fprintf(w, "%-14s %-6s %-9s %s\n", st.Name, st.Mode, yesNo(st.Detected), status)
	}
	return nil
}

func listStatus(st adapter.AgentStatus, name string) string {
	switch {
	case st.Problem != "":
		return "UNHEALTHY: " + st.Problem
	case st.Mode == adapter.ModeAudit:
		return "audit mode — no pre-tool hook surface (docs/install/zed.md)"
	case !st.Detected:
		return fmt.Sprintf("agent not detected — after installing it, run `glove install %s`", name)
	case st.HookInstalled:
		return fmt.Sprintf("hook entry installed ✓ (%s) — %s", st.Entry, st.ConfigPath)
	default:
		return fmt.Sprintf("hook entry NOT installed (run `glove install %s`)", name)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// runMatrix prints the coverage matrix; with check it machine-verifies
// README.md against the generated table and exits 1 on drift — the CI gate
// behind PRD-0.5.0-6 ("README coverage matrix is machine-verified by
// `glove list --matrix`").
func runMatrix(w io.Writer, check bool) error {
	table := renderMatrix()
	if !check {
		fmt.Fprint(w, table)
		return nil
	}
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	doc := string(raw)
	begin := strings.Index(doc, matrixBegin)
	end := strings.Index(doc, matrixEnd)
	if begin < 0 || end < 0 || end < begin {
		return fmt.Errorf("%s has no %s … %s block — cannot verify the coverage matrix", readmePath, matrixBegin, matrixEnd)
	}
	inDoc := doc[begin+len(matrixBegin) : end]
	// Normalize line endings (a Windows checkout carries CRLF) and trim the
	// surrounding newlines the README keeps for readability.
	inDoc = strings.ReplaceAll(inDoc, "\r\n", "\n")
	inDoc = strings.Trim(inDoc, "\n")
	table = strings.TrimRight(table, "\n")
	if inDoc != table {
		fmt.Fprintf(w, "coverage matrix drift between %s and the generated table:\n\n", readmePath)
		adapter.WriteDiff(w, adapter.SplitLines(inDoc), adapter.SplitLines(table))
		fmt.Fprintln(w, "\nrun `glove list --matrix` and paste the table into the README block")
		return ExitError(1)
	}
	fmt.Fprintf(w, "coverage matrix verified against %s — %d agents, no \"to be verified\" rows\n", readmePath, len(matrixRows()))
	return nil
}
