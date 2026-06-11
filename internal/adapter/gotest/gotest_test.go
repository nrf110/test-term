package gotest

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

const samplePkg = "go:example.com/sample"

func sampleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "projects", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func requireGo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

func TestDetect(t *testing.T) {
	a := New()
	dir := sampleDir(t)

	// Detect from the module dir, and from a subdir, should both root at the
	// module (the dir containing go.mod).
	for _, from := range []string{dir, filepath.Join(dir, "nonexistent-subpkg")} {
		got, ok := a.Detect(from)
		if !ok {
			t.Fatalf("Detect(%q) = false, want true", from)
		}
		if got.RootDir != dir {
			t.Fatalf("Detect(%q) root = %q, want %q", from, got.RootDir, dir)
		}
	}

	if _, ok := a.Detect(t.TempDir()); ok {
		t.Fatal("Detect in a non-module dir should be false")
	}
}

func TestDiscoverRealModule(t *testing.T) {
	requireGo(t)
	a := New()
	s := engine.New()
	dir := sampleDir(t)

	if err := a.Discover(context.Background(), dir, s.Apply); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, name := range []string{"TestAdd", "TestAlwaysFails", "TestSkipped", "TestWithSubtests"} {
		n := s.Get(samplePkg + "::" + name)
		if n == nil {
			t.Fatalf("%s not discovered", name)
		}
		if n.Status != event.StatusPending {
			t.Errorf("%s status = %s pre-run, want pending", name, n.Status)
		}
		if n.Location == nil || filepath.Base(n.Location.File) != "math_test.go" || n.Location.Line <= 0 {
			t.Errorf("%s location = %+v, want math_test.go:<line>", name, n.Location)
		}
	}
}

func TestRunRealModule(t *testing.T) {
	requireGo(t)
	a := New()
	s := engine.New()
	dir := sampleDir(t)

	if err := a.Discover(context.Background(), dir, s.Apply); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if err := a.Run(context.Background(), dir, adapter.Selection{}, s.Apply); err != nil {
		t.Fatalf("Run: %v", err)
	}

	checks := map[string]event.Status{
		samplePkg + "::TestAdd":              event.StatusPass,
		samplePkg + "::TestAlwaysFails":      event.StatusFail,
		samplePkg + "::TestSkipped":          event.StatusSkip,
		samplePkg + "::TestWithSubtests/ok":  event.StatusPass,
		samplePkg + "::TestWithSubtests/bad": event.StatusFail,
	}
	for id, want := range checks {
		n := s.Get(id)
		if n == nil {
			t.Fatalf("%s missing after run", id)
		}
		if n.Status != want {
			t.Errorf("%s status = %s, want %s", id, n.Status, want)
		}
	}

	// The failing test carries a usable, absolute source location.
	fail := s.Get(samplePkg + "::TestAlwaysFails")
	if fail.Failure == nil || len(fail.Failure.Frames) == 0 {
		t.Fatalf("TestAlwaysFails has no failure frames: %+v", fail.Failure)
	}
	fr := fail.Failure.Frames[0]
	if !filepath.IsAbs(fr.File) || filepath.Base(fr.File) != "math_test.go" {
		t.Errorf("failure frame file = %q, want absolute .../math_test.go", fr.File)
	}

	// Container nodes aggregate to failing.
	if pkg := s.Get(samplePkg); pkg == nil || pkg.Status != event.StatusFail {
		t.Errorf("package node status = %v, want fail", pkg)
	}
	if loc, ok := a.Locate(samplePkg + "::TestAdd"); !ok || filepath.Base(loc.File) != "math_test.go" {
		t.Errorf("Locate(TestAdd) = %+v ok=%v, want math_test.go", loc, ok)
	}
}

func TestRunSelectionSingleTest(t *testing.T) {
	requireGo(t)
	a := New()
	s := engine.New()
	dir := sampleDir(t)

	sel := adapter.Selection{NodeIDs: []string{samplePkg + "::TestAdd"}}
	if err := a.Run(context.Background(), dir, sel, s.Apply); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if n := s.Get(samplePkg + "::TestAdd"); n == nil || n.Status != event.StatusPass {
		t.Fatalf("selected TestAdd: %v, want pass", n)
	}
	// A test that was not selected must not have run.
	if n := s.Get(samplePkg + "::TestAlwaysFails"); n != nil && n.Status.Terminal() {
		t.Errorf("TestAlwaysFails ran (%s) despite not being selected", n.Status)
	}
}

func TestRunArgs(t *testing.T) {
	if got := runArgs(adapter.Selection{}); !equal(got, []string{"test", "-json", "./..."}) {
		t.Errorf("empty selection args = %v", got)
	}

	got := runArgs(adapter.Selection{NodeIDs: []string{"go:p::TestB", "go:p::TestA/sub"}})
	want := []string{"test", "-json", "-run", "^(TestA|TestB)$", "p"}
	if !equal(got, want) {
		t.Errorf("specific-tests args = %v, want %v", got, want)
	}

	// A whole-package selection drops -run so every test in the package runs.
	got = runArgs(adapter.Selection{NodeIDs: []string{"go:p"}})
	if !equal(got, []string{"test", "-json", "p"}) {
		t.Errorf("package selection args = %v", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
