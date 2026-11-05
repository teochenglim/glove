package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/audit"
	"github.com/teochenglim/glove/internal/paths"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect the decision audit log (PRD §5.4)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newAuditTailCmd(), newAuditQueryCmd(), newAuditExportCmd(), newAuditVerifyCmd(), newAuditStatsCmd())
	return cmd
}

// newAuditStatsCmd implements `glove audit stats` (v0.8.0, AC0.8.2): the
// local "SIEM-lite" view — top rules, techniques, agents, and decision
// counts over a window.
func newAuditStatsCmd() *cobra.Command {
	var since string
	var top int
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize the audit log — top rules, techniques, agents, decision counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if top < 0 {
				return fmt.Errorf("--top must be >= 0, got %d", top)
			}
			sinceTs, err := parseSince(since)
			if err != nil {
				return err
			}
			dir := paths.DataDir()
			if dir == "" {
				return fmt.Errorf("no audit directory (no home context)")
			}
			q := audit.Query{Since: sinceTs}
			if sinceTs.IsZero() {
				q = audit.Query{}
			}
			recs, err := audit.QueryRecords(dir, q)
			if err != nil {
				return err
			}
			st := audit.ComputeStats(recs)
			w := cmd.OutOrStdout()
			if st.Records == 0 {
				fmt.Fprintln(w, "no audit records in window")
				return nil
			}
			fmt.Fprintf(w, "records: %d\n", st.Records)
			fmt.Fprintf(w, "decisions: %d allow, %d review, %d block\n",
				st.Decisions["allow"], st.Decisions["review"], st.Decisions["block"])
			printCounts(w, "agents", st.Agents, top)
			printCounts(w, "rules", st.Rules, top)
			printCounts(w, "techniques", st.Techniques, top)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only records newer than this duration (e.g. 24h)")
	cmd.Flags().IntVar(&top, "top", 10, "how many entries to show per table (0 = all)")
	return cmd
}

func printCounts(w io.Writer, label string, counts []audit.Count, top int) {
	if len(counts) == 0 {
		return
	}
	if top > 0 && len(counts) > top {
		counts = counts[:top]
	}
	parts := make([]string, 0, len(counts))
	for _, c := range counts {
		parts = append(parts, fmt.Sprintf("%s %d", c.Name, c.N))
	}
	fmt.Fprintf(w, "top %s: %s\n", label, strings.Join(parts, ", "))
}

func newAuditTailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tail",
		Short: "Show the last 20 audit records, oldest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := paths.DataDir()
			if dir == "" {
				return fmt.Errorf("no audit directory (no home context)")
			}
			recs, err := audit.Tail(dir, 20)
			if err != nil {
				return err
			}
			printRecords(os.Stdout, recs)
			return nil
		},
	}
}

func newAuditQueryCmd() *cobra.Command {
	var decision, agent, technique, since string
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Filter records (--decision allow|review|block, --agent, --technique T1552, --since <duration>)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceTs, err := parseSince(since)
			if err != nil {
				return err
			}
			dir := paths.DataDir()
			if dir == "" {
				return fmt.Errorf("no audit directory (no home context)")
			}
			recs, err := audit.QueryRecords(dir, audit.Query{Decision: decision, Agent: agent, Technique: technique, Since: sinceTs})
			if err != nil {
				return err
			}
			printRecords(os.Stdout, recs)
			return nil
		},
	}
	cmd.Flags().StringVar(&decision, "decision", "", "filter by decision: allow, review or block")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent name (e.g. claude-code)")
	cmd.Flags().StringVar(&technique, "technique", "", "filter by MITRE ATT&CK technique tag (e.g. T1552.001)")
	cmd.Flags().StringVar(&since, "since", "", "only records newer than this duration (e.g. 24h)")
	return cmd
}

func newAuditExportCmd() *cobra.Command {
	var format, since string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export records to stdout (--format jsonl|csv, --since <duration>)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "jsonl", "csv", "cef", "ecs":
			default:
				return fmt.Errorf("--format must be jsonl, csv, cef or ecs, got %q", format)
			}
			sinceTs, err := parseSince(since)
			if err != nil {
				return err
			}
			dir := paths.DataDir()
			if dir == "" {
				return fmt.Errorf("no audit directory (no home context)")
			}
			q := audit.Query{Since: sinceTs}
			if sinceTs.IsZero() {
				q = audit.Query{}
			}
			recs, err := audit.QueryRecords(dir, q)
			if err != nil {
				return err
			}
			switch format {
			case "csv":
				return writeCSV(os.Stdout, recs)
			case "cef":
				for _, r := range recs {
					fmt.Println(audit.CEF(r))
				}
				return nil
			case "ecs":
				for _, r := range recs {
					b, err := audit.ECS(r)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				}
				return nil
			default:
				for _, r := range recs {
					b, err := json.Marshal(r)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "jsonl", "output format: jsonl, csv, cef or ecs")
	cmd.Flags().StringVar(&since, "since", "", "only records newer than this duration (e.g. 24h)")
	return cmd
}

// newAuditVerifyCmd walks the tamper-evidence chain (v0.7.0 AC0.7.1) and
// exits 1 naming the first broken record when the log fails verification.
func newAuditVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit hash chain — detects edited, deleted or truncated records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := paths.DataDir()
			if dir == "" {
				return fmt.Errorf("no audit directory (no home context)")
			}
			rep, err := audit.Verify(dir)
			if err != nil {
				return err
			}
			if rep.Ok() {
				fmt.Fprintf(os.Stdout, "ok: %d record(s) verified (%d legacy pre-0.7.0, unchained)\n", rep.Verified, rep.Legacy)
				return nil
			}
			b := rep.Breaks[0]
			detail := b.Detail
			if detail != "" {
				detail = " — " + detail
			}
			return fmt.Errorf("TAMPERED: %s:%d %s%s (%d record(s) verified before the break)",
				b.File, b.Line, b.Kind, detail, rep.Verified)
		},
	}
}

// parseSince converts "24h"-style durations into a cutoff timestamp; empty
// means "no filter".
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("--since must be a positive duration (e.g. 24h), got %q", s)
	}
	return time.Now().Add(-d), nil
}

// printRecords renders a compact table; non-decision events (policy_reload)
// are shown with their reason instead of a decision.
func printRecords(w io.Writer, recs []audit.Record) {
	if len(recs) == 0 {
		fmt.Fprintln(w, "no audit records")
		return
	}
	for _, r := range recs {
		ts := r.Ts.Format(time.RFC3339)
		if r.Event != "" {
			fmt.Fprintf(w, "%s  event      %s  %s\n", ts, r.Event, strings.Join(r.Reasons, "; "))
			continue
		}
		reason := ""
		if len(r.Reasons) > 0 {
			reason = r.Reasons[0]
		}
		fmt.Fprintf(w, "%s  %-8s  %-8s  %-6s  %s\n", ts, r.Decision, r.Risk, r.Tool, reason)
	}
}

// writeCSV exports the §5.4 record columns (reasons joined with "; ").
func writeCSV(w io.Writer, recs []audit.Record) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"ts", "audit_id", "agent", "session_id", "tool", "input_hash",
		"decision", "risk", "confidence", "deterministic", "model_available",
		"reasons", "latency_ms", "glove_version", "event",
	}); err != nil {
		return err
	}
	for _, r := range recs {
		if err := cw.Write([]string{
			r.Ts.Format(time.RFC3339Nano), r.AuditID, r.Agent, r.SessionID,
			r.Tool, r.InputHash, r.Decision, r.Risk,
			fmt.Sprintf("%.2f", r.Confidence),
			fmt.Sprintf("%t", r.Deterministic),
			fmt.Sprintf("%t", r.ModelAvailable),
			strings.Join(r.Reasons, "; "),
			fmt.Sprintf("%d", r.LatencyMs),
			r.GloveVersion, r.Event,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
