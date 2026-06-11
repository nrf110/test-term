// Package tui renders the test tree and drives runs. It is a client of the
// engine: it builds its view-model from a subscription snapshot and applies the
// live event stream, exactly as a remote client does — so the same code path
// works locally (Phase 3) and against a remote engine (Phase 4).
package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// Controller starts and cancels runs. Methods are non-blocking: results stream
// back as engine events. The engine layer wraps each run with RunStarted/
// RunFinished, so the TUI learns of run state through the event stream.
type Controller interface {
	Run(sel engine.Selection)
	Cancel()
}

// Model is the root Bubble Tea model.
type Model struct {
	session *engine.Session
	sub     *engine.Subscription
	ctrl    Controller

	project string // shown in the header
	conn    string // connection descriptor, e.g. "local"

	width, height int
	rows          []row
	cursor        int
	offset        int
	expanded      map[string]bool

	filter    textinput.Model
	filtering bool
	running   bool
	showHelp  bool
	notice    string
	quitting  bool

	keys keyMap
	help help.Model
}

// New builds a Model over the given session (an in-process engine or a client's
// mirror — the TUI cannot tell the difference). The TUI is a pure view: it reads
// snapshots from the session and treats the event stream as change signals. The
// session is fed by its owner (a local runner or the client's WebSocket reader),
// so the TUI never applies events itself.
func New(eng *engine.Session, ctrl Controller, project, conn string) Model {
	sub := eng.Subscribe(1024)
	fi := textinput.New()
	fi.Placeholder = "filter tests"
	fi.Prompt = "/"

	m := Model{
		session:  eng,
		sub:      sub,
		ctrl:     ctrl,
		project:  project,
		conn:     conn,
		expanded: map[string]bool{},
		filter:   fi,
		keys:     defaultKeys(),
		help:     help.New(),
	}
	// Roots start expanded so the tree is visible immediately.
	for _, r := range sub.Snapshot {
		m.expanded[r.ID] = true
	}
	m.rebuild()
	return m
}

// eventMsg carries one engine event into the update loop.
type eventMsg event.Event

// eventsClosedMsg signals the subscription ended.
type eventsClosedMsg struct{}

// waitForEvent reads the next event (or subscription close) as a tea.Msg.
func waitForEvent(sub *engine.Subscription) tea.Cmd {
	return func() tea.Msg {
		select {
		case e := <-sub.C:
			return eventMsg(e)
		case <-sub.Done():
			return eventsClosedMsg{}
		}
	}
}

// Init starts the event pump and kicks off an initial full run.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForEvent(m.sub),
		func() tea.Msg { m.ctrl.Run(engine.Selection{All: true}); return nil },
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.clampScroll()
		return m, nil

	case eventMsg:
		// The session owner has already applied this event; we only react to it
		// as a change signal and re-read the live tree.
		switch event.Event(msg).Type {
		case event.TypeRunStarted:
			m.running = true
		case event.TypeRunFinished:
			m.running = false
		}
		m.rebuild()
		return m, waitForEvent(m.sub)

	case eventsClosedMsg:
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey processes a key press, dispatching to filter mode when active.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	m.notice = ""
	k := m.keys
	switch {
	case key.Matches(msg, k.Quit):
		m.quitting = true
		m.sub.Close()
		return m, tea.Quit
	case key.Matches(msg, k.Up):
		m.moveCursor(-1)
	case key.Matches(msg, k.Down):
		m.moveCursor(1)
	case key.Matches(msg, k.Toggle):
		m.toggleExpand()
	case key.Matches(msg, k.Expand):
		m.setExpand(true)
	case key.Matches(msg, k.Collapse):
		m.setExpand(false)
	case key.Matches(msg, k.RunSelected):
		if n := m.selectedNode(); n != nil {
			m.ctrl.Run(engine.Selection{Nodes: []string{n.ID}})
		}
	case key.Matches(msg, k.RunAll):
		m.ctrl.Run(engine.Selection{All: true})
	case key.Matches(msg, k.RerunFailed):
		m.ctrl.Run(engine.Selection{Failed: true})
	case key.Matches(msg, k.Cancel):
		m.ctrl.Cancel()
	case key.Matches(msg, k.Open):
		m.notice = "editor integration arrives in a later phase"
	case key.Matches(msg, k.Filter):
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil
}

// handleFilterKey processes keys while the filter input is focused.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.rebuild()
		return m, nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuild()
	return m, cmd
}
