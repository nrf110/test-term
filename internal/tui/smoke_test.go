package tui

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestProgramRendersAndQuits drives the model through the real Bubble Tea
// runtime: it must render the discovered tree and then exit cleanly on "q".
// This exercises Init/Update/View and the event pump end-to-end, which the
// pure-function tests cannot.
func TestProgramRendersAndQuits(t *testing.T) {
	m := New(seedSession(), &fakeController{}, "/proj", "local", "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("auth.go")) && bytes.Contains(b, []byte("T2"))
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
