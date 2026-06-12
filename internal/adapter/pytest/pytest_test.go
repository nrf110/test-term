package pytest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

func sampleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "projects", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDetect(t *testing.T) {
	a := New()
	dir := sampleDir(t)
	got, ok := a.Detect(dir)
	if !ok || got.RootDir != dir {
		t.Fatalf("Detect(sample) = %+v, %v; want root %s", got, ok, dir)
	}
	if _, ok := a.Detect(t.TempDir()); ok {
		t.Error("Detect on a dir with no tests should be false")
	}
}

// requirePytest returns an adapter backed by the repo's .venv interpreter, or
// skips if it (and pytest) aren't available.
func requirePytest(t *testing.T) (*Adapter, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	py, err := filepath.Abs(filepath.Join("..", "..", "..", ".venv", "bin", "python"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(py); err != nil {
		t.Skip("repo .venv not found (create it and pip install pytest pytest-reportlog)")
	}
	if err := exec.Command(py, "-m", "pytest", "--version").Run(); err != nil {
		t.Skip("pytest not available in .venv")
	}
	return NewWithPython(py), sampleDir(t)
}

func TestEmitDiagnosticExplainsMissingPlugin(t *testing.T) {
	a := New()
	var rec recorder
	a.emitDiagnostic(rec.emit, "pytest: error: unrecognized arguments: --report-log=/tmp/x")

	e, ok := rec.finished(nodeID("<pytest>"))
	if !ok || e.Status != event.StatusError {
		t.Fatalf("expected an error node, got %+v ok=%v", e, ok)
	}
	if e.Failure == nil || !strings.Contains(e.Failure.Message, "pip install pytest-reportlog") {
		t.Fatalf("diagnostic should mention the plugin install, got %+v", e.Failure)
	}
}

func TestDiscoverRealPytest(t *testing.T) {
	a, dir := requirePytest(t)
	s := engine.New()
	if err := a.Discover(context.Background(), dir, s.Apply); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, id := range []string{
		"pytest:test_math.py",
		"pytest:test_math.py::test_add_positive",
		"pytest:test_math.py::TestGroup::test_method",
	} {
		if s.Get(id) == nil {
			t.Errorf("%s not discovered", id)
		}
	}
}

func TestRunRealPytest(t *testing.T) {
	a, dir := requirePytest(t)
	s := engine.New()
	if err := a.Run(context.Background(), dir, adapter.Selection{}, s.Apply); err != nil {
		t.Fatalf("Run: %v", err)
	}

	checks := map[string]event.Status{
		"pytest:test_math.py::test_add_positive":      event.StatusPass,
		"pytest:test_math.py::test_add_fails":         event.StatusFail,
		"pytest:test_math.py::test_skipped":           event.StatusSkip,
		"pytest:test_math.py::test_param[2-3-5]":      event.StatusPass,
		"pytest:test_math.py::TestGroup::test_method": event.StatusPass,
	}
	for id, want := range checks {
		n := s.Get(id)
		if n == nil {
			t.Fatalf("%s missing after run", id)
		}
		if n.Status != want {
			t.Errorf("%s = %s, want %s", id, n.Status, want)
		}
	}

	fail := s.Get("pytest:test_math.py::test_add_fails")
	if fail.Failure == nil || len(fail.Failure.Frames) == 0 {
		t.Fatalf("failing test has no frames: %+v", fail.Failure)
	}
	if !filepath.IsAbs(fail.Failure.Frames[0].File) {
		t.Errorf("frame path %q should be absolute", fail.Failure.Frames[0].File)
	}
	if loc, ok := a.Locate("pytest:test_math.py::test_add_fails"); !ok || loc.Line == 0 {
		t.Errorf("Locate = %+v ok=%v", loc, ok)
	}
}
