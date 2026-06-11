package vitest

import (
	"context"
	"os"
	"path/filepath"
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
		t.Error("Detect on a non-vitest dir should be false")
	}
}

func TestDiscoverGlobsTestFiles(t *testing.T) {
	a := New()
	s := engine.New()
	if err := a.Discover(context.Background(), sampleDir(t), s.Apply); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	n := s.Get("vitest:math.test.js")
	if n == nil {
		t.Fatal("math.test.js not discovered")
	}
	if n.Kind != event.KindFile {
		t.Errorf("kind = %s, want file", n.Kind)
	}
	// node_modules must be skipped — only our one test file should appear.
	for _, root := range s.Snapshot() {
		if root.ID != "vitest:math.test.js" {
			t.Errorf("unexpected discovered file %q (node_modules not skipped?)", root.ID)
		}
	}
}

func requireVitest(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	dir := sampleDir(t)
	if _, err := os.Stat(filepath.Join(dir, "node_modules", ".bin", "vitest")); err != nil {
		t.Skip("vitest not installed in sample project (run npm install there)")
	}
	return dir
}

func TestRunRealVitest(t *testing.T) {
	dir := requireVitest(t)
	a := New()
	s := engine.New()
	if err := a.Run(context.Background(), dir, adapter.Selection{}, s.Apply); err != nil {
		t.Fatalf("Run: %v", err)
	}

	checks := map[string]event.Status{
		"vitest:math.test.js::add::adds positive numbers":   event.StatusPass,
		"vitest:math.test.js::add::fails on purpose":        event.StatusFail,
		"vitest:math.test.js::add::is not ready yet":        event.StatusSkip,
		"vitest:math.test.js::add::when nested::still adds": event.StatusPass,
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

	fail := s.Get("vitest:math.test.js::add::fails on purpose")
	if fail.Failure == nil || len(fail.Failure.Frames) == 0 {
		t.Fatalf("failing test has no frames: %+v", fail.Failure)
	}
	if loc, ok := a.Locate("vitest:math.test.js::add::fails on purpose"); !ok || loc.Line == 0 {
		t.Errorf("Locate returned %+v, ok=%v", loc, ok)
	}
}
