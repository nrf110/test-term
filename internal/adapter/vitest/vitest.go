// Package vitest adapts the Vitest test runner. Discovery globs test files (so
// the file tree appears instantly without invoking Node); execution parses the
// JSON reporter, which is the authoritative source for the describe/it tree.
package vitest

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// Adapter implements adapter.Adapter for Vitest.
type Adapter struct {
	mu   sync.Mutex
	locs map[string]event.Location
}

// New returns a ready Vitest adapter.
func New() *Adapter { return &Adapter{locs: map[string]event.Location{}} }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return "vitest" }

var (
	testFileRe   = regexp.MustCompile(`\.(test|spec)\.(js|jsx|ts|tsx|mjs|cjs)$`)
	configFileRe = regexp.MustCompile(`^v(?:i|ite)test?\.config\.|^vite\.config\.`)
	skipDirs     = map[string]bool{"node_modules": true, ".git": true, "dist": true, "coverage": true, ".vitest": true, "build": true}
)

// Detect reports a Vitest project: the nearest ancestor with a Vitest config or
// a package.json that mentions vitest.
func (a *Adapter) Detect(dir string) (adapter.Detection, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return adapter.Detection{}, false
	}
	for d := abs; ; {
		if hasVitestConfig(d) || packageMentionsVitest(d) {
			return adapter.Detection{RootDir: d}, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return adapter.Detection{}, false
		}
		d = parent
	}
}

func hasVitestConfig(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && configFileRe.MatchString(e.Name()) {
			return true
		}
	}
	return false
}

func packageMentionsVitest(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "vitest")
}

// Discover globs the project for test files and emits a file node for each,
// relative to the project root. Test cases populate when a run executes.
func (a *Adapter) Discover(ctx context.Context, dir string, emit adapter.Emit) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !testFileRe.MatchString(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		id := fileNodeID(rel)
		a.mu.Lock()
		a.locs[id] = event.Location{File: path}
		a.mu.Unlock()
		emit(event.NodeDiscovered(id, "", rel, event.KindFile, &event.Location{File: path}))
		return nil
	})
}

// Run executes the selected tests (all if empty) and parses the JSON reporter.
func (a *Adapter) Run(ctx context.Context, dir string, sel adapter.Selection, emit adapter.Emit) error {
	args := []string{"run", "--reporter=json"}
	args = append(args, selectionFiles(sel)...)

	cmd := vitestCommand(ctx, dir, args)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// A non-zero exit means tests failed — that is normal, and stdout still
		// holds the JSON report. Only a startup failure (not an ExitError) or an
		// exit with no output is a real error.
		if _, ok := err.(*exec.ExitError); !ok || len(out) == 0 {
			return err
		}
	}

	rel := func(abs string) string {
		if r, e := filepath.Rel(dir, abs); e == nil {
			return r
		}
		return abs
	}
	p := newRunParser(emit, rel)
	if perr := p.parse(out); perr != nil {
		return perr
	}

	a.mu.Lock()
	for id, loc := range p.locs {
		a.locs[id] = loc
	}
	a.mu.Unlock()
	return nil
}

// Locate returns a cached source location for a node.
func (a *Adapter) Locate(nodeID string) (event.Location, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	loc, ok := a.locs[nodeID]
	return loc, ok
}

// vitestCommand prefers the project-local vitest binary, falling back to npx.
func vitestCommand(ctx context.Context, dir string, args []string) *exec.Cmd {
	local := filepath.Join(dir, "node_modules", ".bin", "vitest")
	if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
		return exec.CommandContext(ctx, local, args...)
	}
	return exec.CommandContext(ctx, "npx", append([]string{"vitest"}, args...)...)
}

// selectionFiles maps selected node IDs to the distinct files to run. v1 runs at
// file granularity (a finer -t filter is a future refinement).
func selectionFiles(sel adapter.Selection) []string {
	seen := map[string]bool{}
	var files []string
	for _, id := range sel.NodeIDs {
		if rel, ok := splitNodeID(id); ok && !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	return files
}
