// Command tt is a terminal-UI test runner for polyglot projects.
//
// The root command discovers tests in a directory and launches the TUI against
// an in-process engine. Future phases add `serve` (headless engine + MCP) and
// `connect` (TUI against a remote engine) subcommands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/adapter/gotest"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/runner"
	"github.com/nrf110/test-term/internal/tui"
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
		Use:           "tt [path]",
		Short:         "tt — a terminal-UI test runner for polyglot projects",
		Args:          cobra.MaximumNArgs(1),
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			return runTUI(cmd.Context(), cmd, dir)
		},
	}
	cmd.SetVersionTemplate("tt {{.Version}}\n")
	return cmd
}

// registry returns the adapters available in this build.
func registry() *adapter.Registry {
	return adapter.NewRegistry(gotest.New())
}

// runTUI discovers tests under dir and runs the TUI against an in-process engine.
func runTUI(ctx context.Context, cmd *cobra.Command, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	detected := registry().Detect(abs)
	if len(detected) == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No supported test frameworks detected in %s\n", abs)
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	eng := engine.New()
	run := runner.New(ctx, eng, detected)
	if err := run.Discover(ctx); err != nil {
		return fmt.Errorf("discovering tests: %w", err)
	}

	model := tui.New(eng, run, abs, "local")
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err = p.Run()
	return err
}
