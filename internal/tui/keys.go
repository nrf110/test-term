package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap defines the TUI's key bindings. Navigation keys move the cursor; space
// expands/collapses containers; enter runs the selected node's subtree.
type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	Toggle      key.Binding
	Expand      key.Binding
	Collapse    key.Binding
	RunSelected key.Binding
	RunAll      key.Binding
	RerunFailed key.Binding
	Open        key.Binding
	Cancel      key.Binding
	Filter      key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Toggle:      key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "expand/collapse")),
		Expand:      key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		Collapse:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		RunSelected: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "run node")),
		RunAll:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "run all")),
		RerunFailed: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rerun failed")),
		Open:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in editor")),
		Cancel:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel run")),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.RunAll, k.RerunFailed, k.Open, k.Filter, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Toggle, k.Expand, k.Collapse},
		{k.RunSelected, k.RunAll, k.RerunFailed, k.Cancel},
		{k.Open, k.Filter, k.Help, k.Quit},
	}
}
