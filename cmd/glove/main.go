// Command glove is the guard AI agents wear to act safely: installed at the
// PreToolUse hook point it intercepts every tool invocation before execution,
// decides allow / review / block, and writes an immutable audit record.
//
// Thin wiring only (PRD §4.3): the command surface lives in internal/cli and
// is frozen at 1.0.0 (PRD §6).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/teochenglim/glove/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		var ee cli.ExitError
		if errors.As(err, &ee) {
			os.Exit(int(ee))
		}
		fmt.Fprintln(os.Stderr, "glove:", err)
		os.Exit(1)
	}
}
