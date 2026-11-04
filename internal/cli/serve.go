package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teochenglim/glove/internal/server"
)

// newServeCmd implements `glove serve` (PRD §3.2, §7 0.4.0): the Layer 2
// daemon. Thin wiring — the loopback enforcement and the laya container
// contract (ADR 3) live in internal/server.
func newServeCmd() *cobra.Command {
	var port int
	var endpoint string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the decision daemon (Layer 2 via the laya container — ADR 3)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadRuntime("")
			if err != nil {
				return fmt.Errorf("policy: %w", err)
			}
			if endpoint == "" {
				endpoint = rt.Policy.Laya.Endpoint
			}
			opts := server.Options{
				Port:        port,
				Endpoint:    endpoint,
				Image:       rt.Policy.Laya.Image,
				ImageDigest: rt.Policy.Laya.ImageDigest,
				ReviewBand:  rt.Policy.Laya.ConfidenceReviewBand,
				Stdin:       os.Stdin,
				Stdout:      os.Stdout,
				Stderr:      os.Stderr,
			}
			srv, err := server.New(opts)
			if err != nil {
				return err
			}
			return srv.Run(context.Background(), opts)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8799, "loopback port to listen on (127.0.0.1 only)")
	cmd.Flags().StringVar(&endpoint, "laya-endpoint", "", "laya-serve endpoint (default [laya].endpoint or http://127.0.0.1:8080)")
	return cmd
}
