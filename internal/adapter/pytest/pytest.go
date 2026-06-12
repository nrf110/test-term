// Package pytest adapts the pytest test runner. Discovery uses
// `pytest --collect-only -q`; execution uses the pytest-reportlog plugin
// (`--report-log`), whose JSONL records are keyed by the same nodeid as
// discovery and stream as tests run.
package pytest

import (
	"bytes"
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

// Adapter implements adapter.Adapter for pytest.
type Adapter struct {
	python string // interpreter used as "<python> -m pytest"

	mu   sync.Mutex
	locs map[string]event.Location
}

// New returns a pytest adapter using "python3".
func New() *Adapter { return &Adapter{python: "python3", locs: map[string]event.Location{}} }

// NewWithPython returns an adapter using a specific interpreter (e.g. a venv),
// primarily for tests.
func NewWithPython(python string) *Adapter {
	return &Adapter{python: python, locs: map[string]event.Location{}}
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return "pytest" }

var (
	testFileRe  = regexp.MustCompile(`(^test_.*\.py$)|(.*_test\.py$)`)
	configFiles = map[string]bool{"pytest.ini": true, "conftest.py": true, "tox.ini": true}
	skipDirs    = map[string]bool{".venv": true, "venv": true, ".git": true, "node_modules": true, "__pycache__": true, ".tox": true, ".mypy_cache": true, "testdata": true, "vendor": true}
)

// Detect reports a pytest project rooted at dir if it contains a pytest config
// or any test file in its tree.
func (a *Adapter) Detect(dir string) (adapter.Detection, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return adapter.Detection{}, false
	}
	if hasPytestSignal(abs) {
		return adapter.Detection{RootDir: abs}, true
	}
	return adapter.Detection{}, false
}

func hasPytestSignal(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if configFiles[name] || testFileRe.MatchString(name) || mentionsPytest(name, path) {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// mentionsPytest checks config files that only count if they reference pytest.
func mentionsPytest(name, path string) bool {
	if name != "pyproject.toml" && name != "setup.cfg" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), "pytest")
}

// Discover collects tests without running them.
func (a *Adapter) Discover(ctx context.Context, dir string, emit adapter.Emit) error {
	cmd := a.command(ctx, dir, "--collect-only", "-q")
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok || len(out) == 0 {
			return err
		}
	}
	return parseCollect(strings.NewReader(string(out)), emit)
}

// Run executes the selected tests (all if empty) via the report-log plugin.
func (a *Adapter) Run(ctx context.Context, dir string, sel adapter.Selection, emit adapter.Emit) error {
	logFile, err := os.CreateTemp("", "tt-pytest-*.jsonl")
	if err != nil {
		return err
	}
	logPath := logFile.Name()
	_ = logFile.Close()
	defer func() { _ = os.Remove(logPath) }()

	args := append([]string{"--report-log=" + logPath, "-q"}, selectionNodeIDs(sel)...)
	cmd := a.command(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		if _, ok := runErr.(*exec.ExitError); !ok {
			return runErr // pytest could not start at all
		}
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	// An empty report despite a run usually means the pytest-reportlog plugin
	// isn't installed (so --report-log was rejected). Surface that as a visible
	// diagnostic node rather than failing silently.
	if len(bytes.TrimSpace(data)) == 0 {
		a.emitDiagnostic(emit, stderr.String())
		return nil
	}
	p := newRunParser(emit, dir)
	if perr := p.parse(strings.NewReader(string(data))); perr != nil {
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

// emitDiagnostic surfaces a setup problem (e.g. a missing plugin) as an error
// node so the user sees an explanation in the tree instead of an empty run.
func (a *Adapter) emitDiagnostic(emit adapter.Emit, stderr string) {
	msg := strings.TrimSpace(stderr)
	if strings.Contains(msg, "report-log") || strings.Contains(msg, "reportlog") || msg == "" {
		hint := "pytest produced no report. The pytest-reportlog plugin is required:\n\n    pip install pytest-reportlog"
		if msg != "" {
			hint += "\n\n" + msg
		}
		msg = hint
	}
	id := nodeID("<pytest>")
	emit(event.NodeDiscovered(id, "", "pytest (setup error)", event.KindFile, nil))
	emit(event.NodeFinished(id, event.StatusError, 0, &event.Failure{Message: msg}))
}

func (a *Adapter) command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := append([]string{"-m", "pytest"}, args...)
	cmd := exec.CommandContext(ctx, a.python, full...)
	cmd.Dir = dir
	return cmd
}

// selectionNodeIDs maps selected node IDs back to pytest nodeids to pass as args.
func selectionNodeIDs(sel adapter.Selection) []string {
	var out []string
	for _, id := range sel.NodeIDs {
		if nid, ok := toPytestNodeID(id); ok {
			out = append(out, nid)
		}
	}
	return out
}
