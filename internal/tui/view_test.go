package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/nrf110/test-term/internal/engine"
)

// fakeController records run requests without executing anything.
type fakeController struct {
	runs     []engine.Selection
	canceled int
}

func (f *fakeController) Run(sel engine.Selection) { f.runs = append(f.runs, sel) }
func (f *fakeController) Cancel()                  { f.canceled++ }

func newTestModel(t *testing.T, ctrl Controller) Model {
	t.Helper()
	m := New(seedSession(), ctrl, "/proj", "local", "")
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return nm.(Model)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func send(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func TestLayoutFitsTerminalHeight(t *testing.T) {
	for _, h := range []int{6, 8, 10, 24, 50} {
		m := Model{height: h}
		th, dh := m.layout()
		// header(1) + sep(1) + tree + sep(1) + detail + footer(1) must fit.
		if total := th + dh + 4; total > h {
			t.Errorf("height %d: layout total %d (tree %d + detail %d + 4) overflows", h, total, th, dh)
		}
		if th < 1 || dh < 1 {
			t.Errorf("height %d: tree %d / detail %d must each be >= 1", h, th, dh)
		}
	}
}

func TestInitialViewRendersTree(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	if len(m.rows) != 5 {
		t.Fatalf("rows = %d (%v), want 5", len(m.rows), rowIDs(m.rows))
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"auth.go", "T1", "T2", "util.go", "T3", "✓", "✗", "/proj", "local"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n---\n%s", want, out)
		}
	}
}

func TestNavigationMovesCursor(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 2 || m.selectedNode().ID != "f::T2" {
		t.Fatalf("after two downs cursor=%d sel=%v, want 2/f::T2", m.cursor, m.selectedNode())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("after up cursor=%d, want 1", m.cursor)
	}
}

func TestCollapseAndExpandRoot(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	// Cursor on file f; collapse with 'h' hides T1/T2.
	m = send(m, runes("h"))
	if got := rowIDs(m.rows); !equalStrings(got, []string{"f", "g", "g::T3"}) {
		t.Fatalf("after collapse rows = %v", got)
	}
	// Expand again with 'l'.
	m = send(m, runes("l"))
	if got := rowIDs(m.rows); len(got) != 5 {
		t.Fatalf("after expand rows = %v, want 5", got)
	}
}

func TestRunKeybindingsIssueSelections(t *testing.T) {
	fc := &fakeController{}
	m := newTestModel(t, fc)

	m = send(m, runes("a"))                 // run all
	m = send(m, runes("r"))                 // rerun failed
	send(m, tea.KeyMsg{Type: tea.KeyEnter}) // run selected (cursor on f)

	if len(fc.runs) != 3 {
		t.Fatalf("controller runs = %d, want 3", len(fc.runs))
	}
	if !fc.runs[0].All {
		t.Errorf("run[0] = %+v, want All", fc.runs[0])
	}
	if !fc.runs[1].Failed {
		t.Errorf("run[1] = %+v, want Failed", fc.runs[1])
	}
	if len(fc.runs[2].Nodes) != 1 || fc.runs[2].Nodes[0] != "f" {
		t.Errorf("run[2] = %+v, want Nodes=[f]", fc.runs[2])
	}
}

func TestCancelKeybinding(t *testing.T) {
	fc := &fakeController{}
	m := newTestModel(t, fc)
	send(m, runes("c"))
	if fc.canceled != 1 {
		t.Fatalf("cancel count = %d, want 1", fc.canceled)
	}
}

func TestFilterNarrowsTree(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	m = send(m, runes("/"))
	if !m.filtering {
		t.Fatal("expected filtering mode after /")
	}
	m = send(m, runes("T"))
	m = send(m, runes("2"))
	if got := rowIDs(m.rows); !equalStrings(got, []string{"f", "f::T2"}) {
		t.Fatalf("filtered rows = %v, want [f f::T2]", got)
	}
	// Esc clears the filter and restores the full tree.
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filtering || len(m.rows) != 5 {
		t.Fatalf("after esc filtering=%v rows=%d, want false/5", m.filtering, len(m.rows))
	}
}

func TestDetailShowsFailureMessageAndLocation(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	m = send(m, tea.KeyMsg{Type: tea.KeyDown}) // select f::T2 (failing)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "expected 401 got 200") {
		t.Errorf("detail missing failure message\n%s", out)
	}
	if !strings.Contains(out, "auth_test.go:42:7") {
		t.Errorf("detail missing clickable location\n%s", out)
	}
}

func TestOpenWithNoEditorShowsNotice(t *testing.T) {
	m := newTestModel(t, &fakeController{}) // model built with editorCmd == ""
	// Select T1, which has a source location, so we reach the editor check.
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedNode().ID != "f::T1" {
		t.Fatalf("expected cursor on f::T1, got %s", m.selectedNode().ID)
	}
	m = send(m, runes("o"))
	if !strings.Contains(m.notice, "no editor configured") {
		t.Fatalf("notice = %q, want no-editor message", m.notice)
	}
	if !strings.Contains(ansi.Strip(m.View()), "no editor configured") {
		t.Error("footer should show the no-editor notice")
	}
}

func TestOpenWithoutLocationShowsNotice(t *testing.T) {
	m := newTestModel(t, &fakeController{})
	// Cursor starts on file node f, which has no source location in the seed.
	m = send(m, runes("o"))
	if !strings.Contains(m.notice, "no source location") {
		t.Fatalf("notice = %q, want no-location message", m.notice)
	}
}
