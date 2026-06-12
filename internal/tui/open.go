package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nrf110/test-term/internal/locate"
)

// editorClosedMsg reports that the spawned editor has exited.
type editorClosedMsg struct{ err error }

// openSelected opens the selected node's source location in the configured
// editor via tea.ExecProcess, which suspends the TUI, hands the terminal to the
// editor (needed for terminal editors like nvim), and resumes on exit.
func (m Model) openSelected() (tea.Model, tea.Cmd) {
	n := m.selectedNode()
	if n == nil {
		return m, nil
	}
	loc, ok := primaryLocation(n)
	if !ok || loc.File == "" {
		m.notice = "no source location for this item"
		return m, nil
	}
	if m.editorCmd == "" {
		m.notice = "no editor configured — set $EDITOR or ~/.config/tt/config.toml"
		return m, nil
	}

	cmd, err := locate.Command(m.editorCmd, locate.Target{File: loc.File, Line: loc.Line, Col: loc.Col})
	if err != nil {
		m.notice = "open failed: " + err.Error()
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorClosedMsg{err: err} })
}
