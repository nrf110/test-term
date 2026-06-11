package tui

import (
	"testing"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// seedSession builds a small two-file tree used across TUI tests:
//
//	auth.go (file)
//	  T1 (test, pass)
//	  T2 (test, fail)
//	util.go (file)
//	  T3 (test)
func seedSession() *engine.Session {
	s := engine.New()
	s.Apply(event.NodeDiscovered("f", "", "auth.go", event.KindFile, nil))
	s.Apply(event.NodeDiscovered("f::T1", "f", "T1", event.KindTest, &event.Location{File: "auth_test.go", Line: 5}))
	s.Apply(event.NodeDiscovered("f::T2", "f", "T2", event.KindTest, nil))
	s.Apply(event.NodeDiscovered("g", "", "util.go", event.KindFile, nil))
	s.Apply(event.NodeDiscovered("g::T3", "g", "T3", event.KindTest, nil))
	s.Apply(event.NodeFinished("f::T1", event.StatusPass, 12, nil))
	s.Apply(event.NodeFinished("f::T2", event.StatusFail, 8, &event.Failure{
		Message: "expected 401 got 200",
		Frames:  []event.Frame{{File: "/abs/auth_test.go", Line: 42, Col: 7}},
	}))
	return s
}

func rowIDs(rows []row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.node.ID
	}
	return out
}

func TestBuildRowsExpandedShowsChildren(t *testing.T) {
	roots := seedSession().Snapshot()
	expanded := map[string]bool{"f": true, "g": true}
	got := rowIDs(buildRows(roots, expanded, ""))
	want := []string{"f", "f::T1", "f::T2", "g", "g::T3"}
	if !equalStrings(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestBuildRowsCollapsedHidesChildren(t *testing.T) {
	roots := seedSession().Snapshot()
	expanded := map[string]bool{"f": false, "g": true}
	got := rowIDs(buildRows(roots, expanded, ""))
	want := []string{"f", "g", "g::T3"}
	if !equalStrings(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestBuildRowsFilterKeepsMatchAndAncestors(t *testing.T) {
	roots := seedSession().Snapshot()
	// Filtering for "T2" shows its parent file and the match; unrelated nodes drop.
	got := rowIDs(buildRows(roots, map[string]bool{}, "T2"))
	want := []string{"f", "f::T2"}
	if !equalStrings(got, want) {
		t.Fatalf("filtered rows = %v, want %v", got, want)
	}
}

func TestBuildRowsFilterRevealsMatchUnderItsParent(t *testing.T) {
	roots := seedSession().Snapshot()
	// Filtering narrows to matches and their ancestor path: matching T3 reveals
	// its parent file g, but not unrelated file f.
	got := rowIDs(buildRows(roots, map[string]bool{}, "T3"))
	want := []string{"g", "g::T3"}
	if !equalStrings(got, want) {
		t.Fatalf("filtered rows = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
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
