package mcpsrv

import (
	"context"
	"testing"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// fakeCommander drives the engine synchronously when Run is called, simulating a
// runner that finishes a test and emits RunFinished.
type fakeCommander struct {
	eng      *engine.Session
	lastSel  engine.Selection
	cancels  int
	finishAs event.Status
}

func (f *fakeCommander) Run(sel engine.Selection) {
	f.lastSel = sel
	f.eng.Apply(event.RunStarted("r", "x"))
	status := f.finishAs
	if status == "" {
		status = event.StatusFail
	}
	var fail *event.Failure
	if status.Failing() {
		fail = &event.Failure{Message: "boom", Frames: []event.Frame{{File: "/a/x_test.go", Line: 9, Col: 2}}}
	}
	f.eng.Apply(event.NodeFinished("go:p::T", status, 2, fail))
	f.eng.Apply(event.RunFinished("r", f.eng.Summary()))
}
func (f *fakeCommander) Cancel() { f.cancels++ }

func setup(t *testing.T) (*tools, *fakeCommander) {
	t.Helper()
	eng := engine.New()
	eng.Apply(event.NodeDiscovered("go:p", "", "p", event.KindFile, nil))
	eng.Apply(event.NodeDiscovered("go:p::T", "go:p", "T", event.KindTest,
		&event.Location{File: "/a/x_test.go", Line: 9}))
	fc := &fakeCommander{eng: eng}
	return &tools{eng: eng, ctrl: fc}, fc
}

func TestListTests(t *testing.T) {
	tl, _ := setup(t)
	_, out, err := tl.listTests(context.Background(), nil, noInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tests) != 2 {
		t.Fatalf("tests = %+v, want 2 (file + test)", out.Tests)
	}
	if out.Tests[0].ID != "go:p" || out.Tests[1].ID != "go:p::T" || out.Tests[1].ParentID != "go:p" {
		t.Fatalf("flattened nodes = %+v, want go:p then go:p::T (parent go:p)", out.Tests)
	}
	if out.Summary.Total != 1 {
		t.Fatalf("summary total = %d, want 1", out.Summary.Total)
	}
}

func TestRunTestsWaitsAndReportsFailures(t *testing.T) {
	tl, fc := setup(t)
	_, out, err := tl.runTests(context.Background(), nil, RunInput{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !fc.lastSel.All {
		t.Errorf("selection = %+v, want All", fc.lastSel)
	}
	if out.Summary.Failed != 1 {
		t.Errorf("summary failed = %d, want 1", out.Summary.Failed)
	}
	if len(out.Failures) != 1 {
		t.Fatalf("failures = %d, want 1", len(out.Failures))
	}
	f := out.Failures[0]
	if f.NodeID != "go:p::T" || f.Message != "boom" || f.File != "/a/x_test.go" || f.Line != 9 || f.Col != 2 {
		t.Fatalf("failure = %+v, want flattened T detail", f)
	}
}

func TestRunTestsDefaultsToAll(t *testing.T) {
	tl, fc := setup(t)
	if _, _, err := tl.runTests(context.Background(), nil, RunInput{}); err != nil {
		t.Fatal(err)
	}
	if !fc.lastSel.All {
		t.Fatalf("empty input selection = %+v, want All defaulted", fc.lastSel)
	}
}

func TestRerunFailedSelectsFailed(t *testing.T) {
	tl, fc := setup(t)
	if _, _, err := tl.rerunFailed(context.Background(), nil, noInput{}); err != nil {
		t.Fatal(err)
	}
	if !fc.lastSel.Failed {
		t.Fatalf("selection = %+v, want Failed", fc.lastSel)
	}
}

func TestGetResultsReportsFailures(t *testing.T) {
	tl, _ := setup(t)
	// No run yet: nothing failing.
	if _, out, _ := tl.getResults(context.Background(), nil, noInput{}); len(out.Failures) != 0 {
		t.Fatalf("pre-run failures = %d, want 0", len(out.Failures))
	}
	// After a run, the failing test shows up.
	_, _, _ = tl.runTests(context.Background(), nil, RunInput{All: true})
	_, out, _ := tl.getResults(context.Background(), nil, noInput{})
	if len(out.Failures) != 1 || out.Failures[0].NodeID != "go:p::T" {
		t.Fatalf("results failures = %+v, want [go:p::T]", out.Failures)
	}
}

func TestGetFailureFoundAndMissing(t *testing.T) {
	tl, _ := setup(t)
	_, _, _ = tl.runTests(context.Background(), nil, RunInput{All: true})

	_, found, _ := tl.getFailure(context.Background(), nil, FailureInput{NodeID: "go:p::T"})
	if !found.Found || found.Failure == nil || found.Failure.Message != "boom" {
		t.Fatalf("getFailure(go:p::T) = %+v, want found with message", found)
	}

	_, missing, _ := tl.getFailure(context.Background(), nil, FailureInput{NodeID: "nope"})
	if missing.Found {
		t.Fatalf("getFailure(nope) = %+v, want not found", missing)
	}
}

func TestCancelRun(t *testing.T) {
	tl, fc := setup(t)
	if _, out, _ := tl.cancelRun(context.Background(), nil, noInput{}); !out.Canceled {
		t.Fatal("cancel output should report canceled")
	}
	if fc.cancels != 1 {
		t.Fatalf("cancel count = %d, want 1", fc.cancels)
	}
}
