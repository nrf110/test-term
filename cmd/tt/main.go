// Command tt is a terminal-UI test runner for polyglot projects.
//
// Three modes share one engine-over-WebSocket code path:
//   - tt [path]        discover + run an in-process engine on a loopback port,
//     and attach the TUI as a client (local == remote, minus
//     the address).
//   - tt serve         run a headless engine for remote/agent access.
//   - tt connect addr  attach the TUI to a remote engine.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/adapter/gotest"
	"github.com/nrf110/test-term/internal/client"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/runner"
	"github.com/nrf110/test-term/internal/server"
	"github.com/nrf110/test-term/internal/tui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

const defaultServeAddr = "127.0.0.1:7878"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tt [path]",
		Short:         "tt — a terminal-UI test runner for polyglot projects",
		Args:          cobra.MaximumNArgs(1),
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocal(cmd, dirArg(args))
		},
	}
	root.SetVersionTemplate("tt {{.Version}}\n")
	root.AddCommand(newServeCmd(), newConnectCmd())
	return root
}

func newServeCmd() *cobra.Command {
	var addr, token string
	cmd := &cobra.Command{
		Use:           "serve [path]",
		Short:         "Run a headless engine other clients/agents can attach to",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, dirArg(args), addr, token)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultServeAddr, "address to listen on")
	cmd.Flags().StringVar(&token, "token", "", "shared secret required for non-loopback addresses")
	return cmd
}

func newConnectCmd() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:           "connect <addr>",
		Short:         "Attach the TUI to a remote engine",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnect(cmd, args[0], token)
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "shared secret if the server requires one")
	return cmd
}

func dirArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

// registry returns the adapters available in this build.
func registry() *adapter.Registry {
	return adapter.NewRegistry(gotest.New())
}

// setupEngine detects frameworks under dir, discovers tests, and returns a ready
// engine + runner. ok is false (with a message printed) if nothing is detected.
func setupEngine(ctx context.Context, cmd *cobra.Command, dir string) (*engine.Session, *runner.Runner, string, bool, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, "", false, err
	}
	detected := registry().Detect(abs)
	if len(detected) == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No supported test frameworks detected in %s\n", abs)
		return nil, nil, abs, false, nil
	}
	eng := engine.New()
	run := runner.New(ctx, eng, detected)
	if err := run.Discover(ctx); err != nil {
		return nil, nil, abs, false, fmt.Errorf("discovering tests: %w", err)
	}
	return eng, run, abs, true, nil
}

// runLocal starts an in-process engine on a loopback port and attaches the TUI
// as a WebSocket client — the same path used for remote, minus the address.
func runLocal(cmd *cobra.Command, dir string) error {
	ctx, stop := signal.NotifyContext(baseCtx(cmd), os.Interrupt)
	defer stop()

	eng, run, abs, ok, err := setupEngine(ctx, cmd, dir)
	if err != nil || !ok {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: server.New(eng, run, "").Handler()}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Close() }()

	cl, err := client.Dial(ctx, ln.Addr().String(), "")
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()

	return runProgram(ctx, cl, abs, "local")
}

// runServe runs the headless engine, blocking until interrupted.
func runServe(cmd *cobra.Command, dir, addr, token string) error {
	if isExposed(addr) && token == "" {
		return fmt.Errorf("refusing to listen on non-loopback address %q without --token", addr)
	}

	ctx, stop := signal.NotifyContext(baseCtx(cmd), os.Interrupt)
	defer stop()

	eng, run, _, ok, err := setupEngine(ctx, cmd, dir)
	if err != nil || !ok {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "tt serve listening on %s\n", ln.Addr())
	if !isExposed(addr) {
		_, _ = fmt.Fprintf(out, "Attach locally with: tt connect %s\n", ln.Addr())
		_, _ = fmt.Fprintf(out, "Attach remotely by tunneling, e.g.: ssh -L %s <host>\n", ln.Addr())
	}

	httpSrv := &http.Server{Handler: server.New(eng, run, token).Handler()}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// runConnect attaches the TUI to a remote engine.
func runConnect(cmd *cobra.Command, addr, token string) error {
	ctx, stop := signal.NotifyContext(baseCtx(cmd), os.Interrupt)
	defer stop()

	cl, err := client.Dial(ctx, addr, token)
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()

	return runProgram(ctx, cl, addr, "remote "+addr)
}

// runProgram launches the TUI against a connected client.
func runProgram(ctx context.Context, cl *client.Client, project, conn string) error {
	model := tui.New(cl.Engine(), cl, project, conn)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

func baseCtx(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// isExposed reports whether addr binds something other than loopback, which
// requires a token.
func isExposed(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}
