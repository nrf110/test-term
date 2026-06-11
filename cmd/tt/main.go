// Command tt is a terminal-UI test runner for polyglot projects.
//
// This is the Phase 0 scaffold: the root command wires up version reporting
// and help. Subsequent phases attach the real behavior — the default command
// will discover tests and launch the TUI (Phase 3), and `serve`/`connect`
// subcommands will host and attach to a remote engine (Phase 4).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd builds the root `tt` command. It is constructed in a function
// (rather than a package-level var) so tests can execute a fresh instance with
// isolated I/O and arguments.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "tt",
		Short:         "tt — a terminal-UI test runner for polyglot projects",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Phase 3 replaces this with test discovery + TUI launch.
			return cmd.Help()
		},
	}
	cmd.SetVersionTemplate("tt {{.Version}}\n")
	return cmd
}
