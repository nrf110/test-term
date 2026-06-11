package vitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrf110/test-term/internal/event"
)

type recorder struct{ events []event.Event }

func (r *recorder) emit(e event.Event) { r.events = append(r.events, e) }

func (r *recorder) discovered(id string) (event.Event, bool) {
	for _, e := range r.events {
		if e.Type == event.TypeNodeDiscovered && e.NodeID == id {
			return e, true
		}
	}
	return event.Event{}, false
}

func (r *recorder) finished(id string) (event.Event, bool) {
	for _, e := range r.events {
		if e.Type == event.TypeNodeFinished && e.NodeID == id {
			return e, true
		}
	}
	return event.Event{}, false
}

func TestParseRunReport(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "run.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var rec recorder
	// The fixture's absolute paths are rooted at the placeholder "/PROJECT".
	rel := func(abs string) string {
		r, _ := filepath.Rel("/PROJECT", abs)
		return r
	}
	p := newRunParser(rec.emit, rel)
	if err := p.parse(data); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Structure: file -> suite -> (tests, nested suite -> test).
	for _, id := range []string{
		"vitest:math.test.js",
		"vitest:math.test.js::add",
		"vitest:math.test.js::add::when nested",
	} {
		if _, ok := rec.discovered(id); !ok {
			t.Errorf("expected discovered node %q", id)
		}
	}
	if e, _ := rec.discovered("vitest:math.test.js::add::when nested"); e.NodeKind != event.KindSuite {
		t.Errorf("nested describe should be a suite, got %s", e.NodeKind)
	}

	// Statuses.
	checks := map[string]event.Status{
		"vitest:math.test.js::add::adds positive numbers":   event.StatusPass,
		"vitest:math.test.js::add::fails on purpose":        event.StatusFail,
		"vitest:math.test.js::add::is not ready yet":        event.StatusSkip,
		"vitest:math.test.js::add::when nested::still adds": event.StatusPass,
	}
	for id, want := range checks {
		e, ok := rec.finished(id)
		if !ok {
			t.Fatalf("missing finished event for %q", id)
		}
		if e.Status != want {
			t.Errorf("%q status = %s, want %s", id, e.Status, want)
		}
	}

	// Failure detail: message + a user-code frame (not node_modules).
	fail, _ := rec.finished("vitest:math.test.js::add::fails on purpose")
	if fail.Failure == nil || !strings.Contains(fail.Failure.Message, "expected 2 to be 3") {
		t.Fatalf("failure message = %+v, want assertion text", fail.Failure)
	}
	if len(fail.Failure.Frames) == 0 {
		t.Fatal("failure should carry a stack frame")
	}
	fr := fail.Failure.Frames[0]
	if !strings.HasSuffix(fr.File, "math.test.js") || strings.Contains(fr.File, "node_modules") || fr.Line != 10 {
		t.Errorf("frame = %+v, want user-code math.test.js:10", fr)
	}
}
