package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/nrf110/test-term/internal/event"
)

// Status colors (256-color palette indices, degrade gracefully on limited terms).
var (
	stylePass     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFail     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleSkip     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSelected = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleBar      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// glyph returns the single-rune status indicator for a node.
func glyph(s event.Status) string {
	switch s {
	case event.StatusPass:
		return "✓"
	case event.StatusFail:
		return "✗"
	case event.StatusError:
		return "!"
	case event.StatusRunning:
		return "⟳"
	case event.StatusSkip:
		return "⊘"
	default:
		return "·"
	}
}

// statusStyle returns the lipgloss style for a status.
func statusStyle(s event.Status) lipgloss.Style {
	switch s {
	case event.StatusPass:
		return stylePass
	case event.StatusFail, event.StatusError:
		return styleFail
	case event.StatusRunning:
		return styleRunning
	case event.StatusSkip:
		return styleSkip
	default:
		return styleDim
	}
}
