package pytest

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
	var got event.Event
	var ok bool
	for _, e := range r.events { // last discovered wins (location fill-in)
		if e.Type == event.TypeNodeDiscovered && e.NodeID == id {
			got, ok = e, true
		}
	}
	return got, ok
}

func (r *recorder) finished(id string) (event.Event, bool) {
	for _, e := range r.events {
		if e.Type == event.TypeNodeFinished && e.NodeID == id {
			return e, true
		}
	}
	return event.Event{}, false
}

func TestParseCollect(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "collect.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rec recorder
	if err := parseCollect(strings.NewReader(string(data)), rec.emit); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"pytest:test_math.py",
		"pytest:test_math.py::test_add_positive",
		"pytest:test_math.py::test_param[1-1-2]",
		"pytest:test_math.py::TestGroup",
		"pytest:test_math.py::TestGroup::test_method",
	}
	for _, id := range want {
		if _, ok := rec.discovered(id); !ok {
			t.Errorf("collect did not discover %q", id)
		}
	}
	if e, _ := rec.discovered("pytest:test_math.py::TestGroup"); e.NodeKind != event.KindSuite {
		t.Errorf("class node kind = %s, want suite", e.NodeKind)
	}
	if e, _ := rec.discovered("pytest:test_math.py::TestGroup::test_method"); e.NodeKind != event.KindTest {
		t.Errorf("method node kind = %s, want test", e.NodeKind)
	}
}

func TestParseReportLog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "report.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rec recorder
	p := newRunParser(rec.emit, "/PROJECT")
	if err := p.parse(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}

	checks := map[string]event.Status{
		"pytest:test_math.py::test_add_positive":      event.StatusPass,
		"pytest:test_math.py::test_add_fails":         event.StatusFail,
		"pytest:test_math.py::test_skipped":           event.StatusSkip,
		"pytest:test_math.py::test_param[1-1-2]":      event.StatusPass,
		"pytest:test_math.py::test_param[2-3-5]":      event.StatusPass,
		"pytest:test_math.py::TestGroup::test_method": event.StatusPass,
	}
	for id, want := range checks {
		e, ok := rec.finished(id)
		if !ok {
			t.Fatalf("missing finished for %q", id)
		}
		if e.Status != want {
			t.Errorf("%q = %s, want %s", id, e.Status, want)
		}
	}

	// Failure detail: reprcrash message + absolute frame at the failing line.
	fail, _ := rec.finished("pytest:test_math.py::test_add_fails")
	if fail.Failure == nil || !strings.Contains(fail.Failure.Message, "assert 2 == 3") {
		t.Fatalf("failure message = %+v, want assert text", fail.Failure)
	}
	if len(fail.Failure.Frames) == 0 {
		t.Fatal("failure should carry a frame")
	}
	fr := fail.Failure.Frames[0]
	if !strings.HasSuffix(fr.File, "test_math.py") || fr.Line != 13 {
		t.Errorf("frame = %+v, want .../test_math.py:13", fr)
	}

	// The failing test node carries a resolved (absolute) location too.
	if loc, ok := p.locs["pytest:test_math.py::test_add_fails"]; !ok || !filepath.IsAbs(loc.File) {
		t.Errorf("cached location = %+v ok=%v, want absolute path", loc, ok)
	}
}
