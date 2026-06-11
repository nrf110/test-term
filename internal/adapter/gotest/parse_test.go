package gotest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrf110/test-term/internal/event"
)

// recorder collects emitted events for assertions.
type recorder struct{ events []event.Event }

func (r *recorder) emit(e event.Event) { r.events = append(r.events, e) }

func (r *recorder) discovered(id string) bool {
	for _, e := range r.events {
		if e.Type == event.TypeNodeDiscovered && e.NodeID == id {
			return true
		}
	}
	return false
}

// finished returns the last NodeFinished event for id.
func (r *recorder) finished(id string) (event.Event, bool) {
	var got event.Event
	var ok bool
	for _, e := range r.events {
		if e.Type == event.TypeNodeFinished && e.NodeID == id {
			got, ok = e, true
		}
	}
	return got, ok
}

func parseFixture(t *testing.T, name string, rec *recorder) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := newRunParser(rec.emit, nil)
	if err := p.parse(strings.NewReader(string(data))); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

func TestParseSimplePackage(t *testing.T) {
	var rec recorder
	parseFixture(t, "simple.jsonl", &rec)

	pkg := "go:example.com/m/calc"
	if !rec.discovered(pkg) {
		t.Fatalf("package node %q not discovered", pkg)
	}

	if e, ok := rec.finished(pkg + "::TestAdd"); !ok || e.Status != event.StatusPass {
		t.Fatalf("TestAdd: got %+v ok=%v, want pass", e, ok)
	}
	if e, ok := rec.finished(pkg + "::TestSkip"); !ok || e.Status != event.StatusSkip {
		t.Fatalf("TestSkip: got %+v ok=%v, want skip", e, ok)
	}

	sub, ok := rec.finished(pkg + "::TestSub")
	if !ok || sub.Status != event.StatusFail {
		t.Fatalf("TestSub: got %+v ok=%v, want fail", sub, ok)
	}
	if sub.Failure == nil || !strings.Contains(sub.Failure.Message, "expected 1, got 2") {
		t.Fatalf("TestSub failure message = %+v, want it to contain the assertion", sub.Failure)
	}
	if len(sub.Failure.Frames) == 0 || sub.Failure.Frames[0].Line != 21 ||
		!strings.HasSuffix(sub.Failure.Frames[0].File, "sub_test.go") {
		t.Fatalf("TestSub frame = %+v, want sub_test.go:21", sub.Failure.Frames)
	}
	// The "=== RUN" / "--- FAIL" framing lines must be excluded from the message.
	if strings.Contains(sub.Failure.Message, "=== RUN") || strings.Contains(sub.Failure.Message, "--- FAIL") {
		t.Fatalf("failure message leaked framing lines: %q", sub.Failure.Message)
	}

	// The package ran tests and one failed, so the container is reported failing.
	if e, ok := rec.finished(pkg); !ok || e.Status != event.StatusFail {
		t.Fatalf("package node: got %+v ok=%v, want fail", e, ok)
	}
}

func TestParseSubtestsDiscoveredMidRun(t *testing.T) {
	var rec recorder
	parseFixture(t, "subtests.jsonl", &rec)

	for _, id := range []string{"go:p::TestParent", "go:p::TestParent/sub_a", "go:p::TestParent/sub_b"} {
		if !rec.discovered(id) {
			t.Fatalf("node %q not discovered", id)
		}
	}
	// Subtest nodes carry their parent.
	var subA event.Event
	for _, e := range rec.events {
		if e.Type == event.TypeNodeDiscovered && e.NodeID == "go:p::TestParent/sub_a" {
			subA = e
		}
	}
	if subA.ParentID != "go:p::TestParent" {
		t.Fatalf("sub_a parent = %q, want go:p::TestParent", subA.ParentID)
	}
	if subA.Name != "sub_a" {
		t.Fatalf("sub_a display name = %q, want sub_a", subA.Name)
	}

	if e, ok := rec.finished("go:p::TestParent/sub_a"); !ok || e.Status != event.StatusPass {
		t.Fatalf("sub_a: %+v ok=%v, want pass", e, ok)
	}
	if e, ok := rec.finished("go:p::TestParent/sub_b"); !ok || e.Status != event.StatusFail {
		t.Fatalf("sub_b: %+v ok=%v, want fail", e, ok)
	}
}

func TestParseBuildFailureSurfacesOnPackage(t *testing.T) {
	var rec recorder
	parseFixture(t, "buildfail.jsonl", &rec)

	e, ok := rec.finished("go:p/broken")
	if !ok || e.Status != event.StatusError {
		t.Fatalf("broken package: %+v ok=%v, want error", e, ok)
	}
	if e.Failure == nil || !strings.Contains(e.Failure.Message, "undefined: Foo") {
		t.Fatalf("build failure message = %+v, want compiler error", e.Failure)
	}
}

func TestAbsResolverAppliedToFrames(t *testing.T) {
	var rec recorder
	abs := func(pkg, file string) string { return "/abs/" + pkg + "/" + file }
	data, _ := os.ReadFile(filepath.Join("testdata", "simple.jsonl"))
	p := newRunParser(rec.emit, abs)
	if err := p.parse(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
	sub, _ := rec.finished("go:example.com/m/calc::TestSub")
	if got := sub.Failure.Frames[0].File; got != "/abs/example.com/m/calc/sub_test.go" {
		t.Fatalf("abs resolver not applied: %q", got)
	}
}
