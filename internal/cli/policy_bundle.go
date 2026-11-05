package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/audit"
	"github.com/teochenglim/glove/internal/paths"
	"github.com/teochenglim/glove/internal/policy"
	"github.com/teochenglim/glove/internal/version"
)

// Policy bundle commands (v0.8.0, AC0.8.3). Bundles are files — pack seals
// a policy with origin metadata and a checksum; import verifies BEFORE any
// write, shows the destination diff, and emits a policy_import audit event
// carrying the checksum.
func newPolicyPackCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pack <policy-file>",
		Short: "Seal a policy.toml into a checksummed bundle with origin metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			dest := out
			if dest == "" {
				dest = src + ".bundle.json"
			}
			b, err := policy.Pack(src, version.Version, time.Now())
			if err != nil {
				return err
			}
			if err := b.SaveTo(dest); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "packed %s -> %s (checksum %s)\n", src, dest, b.Checksum)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "bundle destination (default <policy-file>.bundle.json)")
	return cmd
}

func newPolicyImportCmd() *cobra.Command {
	var out string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <bundle>",
		Short: "Verify a policy bundle and install it (shows the diff; refuses tampered bundles)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := out
			if dest == "" {
				dest = paths.PolicyUserPath()
				if dest == "" {
					return fmt.Errorf("no user policy path (no home context) — pass --out")
				}
			}
			// Verify BEFORE any write (AC0.8.3): a tampered bundle never
			// touches the destination.
			b, err := policy.LoadBundle(args[0])
			if err != nil {
				return err
			}
			// The bundled policy must itself be valid for this glove.
			tmpCheck, err := policy.LoadFileContent([]byte(b.PolicyTOML))
			if err != nil {
				return fmt.Errorf("bundled policy invalid: %w", err)
			}
			oldBytes, readErr := os.ReadFile(dest)
			if readErr != nil && !os.IsNotExist(readErr) {
				return fmt.Errorf("read destination %s: %w", dest, readErr)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bundle verified (checksum %s, packed %s, glove %s)\n",
				b.Checksum, b.Origin.PackedAt.Format(time.RFC3339), b.Origin.GloveVersion)
			adapter.WriteDiff(cmd.OutOrStdout(),
				adapter.SplitLines(string(oldBytes)),
				adapter.SplitLines(b.PolicyTOML))
			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "dry-run: nothing written")
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return fmt.Errorf("create policy dir: %w", err)
			}
			if err := os.WriteFile(dest, []byte(b.PolicyTOML), 0o600); err != nil {
				return fmt.Errorf("write policy %s: %w", dest, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed -> %s (mode=%s, session_threshold resolves to %d)\n",
				dest, tmpCheck.General.Mode, tmpCheck.Correlation.ResolvedThreshold())
			emitPolicyImportEvent(b, dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "destination policy path (default the user policy)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "verify and show the diff only, write nothing")
	return cmd
}

// emitPolicyImportEvent records the import in the local audit trail
// (AC0.8.3) with the bundle checksum. A failed event write is a stderr
// warning — the import already succeeded and the trail hiccup must not
// undo it.
func emitPolicyImportEvent(b *policy.Bundle, dest string) {
	dir := paths.DataDir()
	if dir == "" {
		return
	}
	w := audit.NewWriter(dir)
	_, err := w.Write(audit.Record{
		AuditID: uuid.NewString(),
		Event:   audit.EventPolicyImport,
		Reasons: []string{fmt.Sprintf("policy_import: bundle %s installed to %s (sources: %s)",
			b.Checksum, dest, strings.Join(b.Origin.Sources, ", "))},
		GloveVersion: version.Version,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "glove: policy_import event write failed (import already done): %v\n", err)
	}
}
