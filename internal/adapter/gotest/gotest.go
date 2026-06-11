// Package gotest adapts Go's built-in testing (`go test`) to the normalized
// event model. Discovery uses `go list -json` plus AST parsing (fast, no
// compile, yields source locations); execution streams and parses
// `go test -json`.
package gotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// Adapter implements adapter.Adapter for `go test`.
type Adapter struct {
	mu   sync.Mutex
	dirs map[string]string         // import path -> absolute package dir
	locs map[string]event.Location // node ID -> source location (from discovery)
}

// New returns a ready Go adapter.
func New() *Adapter {
	return &Adapter{dirs: map[string]string{}, locs: map[string]event.Location{}}
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return "go" }

// Detect reports whether dir is inside a Go module, rooting the adapter at the
// module root (the nearest ancestor containing go.mod).
func (a *Adapter) Detect(dir string) (adapter.Detection, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return adapter.Detection{}, false
	}
	for d := abs; ; {
		if fi, err := os.Stat(filepath.Join(d, "go.mod")); err == nil && !fi.IsDir() {
			return adapter.Detection{RootDir: d}, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return adapter.Detection{}, false
		}
		d = parent
	}
}

// Discover lists packages and test functions without running them.
func (a *Adapter) Discover(ctx context.Context, dir string, emit adapter.Emit) error {
	pkgs, err := goList(ctx, dir)
	if err != nil {
		return err
	}
	dirs, locs := discoverPackages(pkgs, emit)

	a.mu.Lock()
	a.dirs = dirs
	a.locs = locs
	a.mu.Unlock()
	return nil
}

// Run executes the selected tests (all if the selection is empty), streaming
// normalized events parsed from `go test -json`.
func (a *Adapter) Run(ctx context.Context, dir string, sel adapter.Selection, emit adapter.Emit) error {
	if err := a.ensureDirs(ctx, dir); err != nil {
		return err
	}

	args := runArgs(sel)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr // build diagnostics flow through; results come via stdout
	if err := cmd.Start(); err != nil {
		return err
	}

	parser := newRunParser(emit, a.absFor)
	parseErr := parser.parse(stdout)

	// A non-zero exit means tests failed — that is a normal result, not a Run
	// error. Only surface failures to start/stream the process.
	if err := cmd.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return err
		}
	}
	return parseErr
}

// Locate returns a node's source location if discovery recorded one.
func (a *Adapter) Locate(nodeID string) (event.Location, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	loc, ok := a.locs[nodeID]
	return loc, ok
}

// ensureDirs populates the import-path→dir map if it is empty (e.g. Run called
// without a prior Discover), so failure file paths can be made absolute.
func (a *Adapter) ensureDirs(ctx context.Context, dir string) error {
	a.mu.Lock()
	empty := len(a.dirs) == 0
	a.mu.Unlock()
	if !empty {
		return nil
	}
	pkgs, err := goList(ctx, dir)
	if err != nil {
		return err
	}
	dirs := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		dirs[p.ImportPath] = p.Dir
	}
	a.mu.Lock()
	a.dirs = dirs
	a.mu.Unlock()
	return nil
}

// absFor resolves a package-relative file (as printed by `go test`) to an
// absolute path using the cached package directory.
func (a *Adapter) absFor(importPath, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	a.mu.Lock()
	dir := a.dirs[importPath]
	a.mu.Unlock()
	if dir == "" {
		return file
	}
	return filepath.Join(dir, file)
}

// runArgs builds the `go test` argument list for a selection.
//
//   - empty selection            -> test the whole module (./...)
//   - specific tests only        -> restrict with -run across the named packages
//   - any whole-package selection -> run the named packages unrestricted
func runArgs(sel adapter.Selection) []string {
	if len(sel.NodeIDs) == 0 {
		return []string{"test", "-json", "./..."}
	}

	var pkgs []string
	seenPkg := map[string]bool{}
	tests := map[string]bool{}
	anyPkgAll := false

	for _, id := range sel.NodeIDs {
		pkg, test, ok := splitNodeID(id)
		if !ok {
			continue
		}
		if !seenPkg[pkg] {
			seenPkg[pkg] = true
			pkgs = append(pkgs, pkg)
		}
		if test == "" {
			anyPkgAll = true
		} else {
			tests[topLevelTest(test)] = true
		}
	}

	if len(pkgs) == 0 {
		return []string{"test", "-json", "./..."}
	}

	args := []string{"test", "-json"}
	if !anyPkgAll && len(tests) > 0 {
		names := make([]string, 0, len(tests))
		for t := range tests {
			names = append(names, t)
		}
		sort.Strings(names)
		args = append(args, "-run", "^("+strings.Join(names, "|")+")$")
	}
	return append(args, pkgs...)
}
