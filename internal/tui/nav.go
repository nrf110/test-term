package tui

import "github.com/nrf110/test-term/internal/engine"

// detailLines is the fixed height of the detail region (title + message body).
const detailLines = 6

// treeHeight is the number of visible tree rows given the current terminal size
// and the fixed header/separator/detail/footer chrome.
func (m Model) treeHeight() int {
	// header(1) + sep(1) + tree + sep(1) + detail(detailLines) + footer(1)
	h := m.height - detailLines - 4
	if h < 1 {
		return 1
	}
	return h
}

// rebuild recomputes the visible row list from the current tree, filter, and
// expansion state, then re-clamps the cursor and scroll.
func (m *Model) rebuild() {
	m.rows = buildRows(m.session.Snapshot(), m.expanded, m.filter.Value())
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

// selectedNode returns the node under the cursor, or nil.
func (m Model) selectedNode() *engine.Node {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].node
	}
	return nil
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	m.clampScroll()
}

// toggleExpand flips expansion of the selected container.
func (m *Model) toggleExpand() {
	n := m.selectedNode()
	if n == nil || len(n.Children) == 0 {
		return
	}
	m.expanded[n.ID] = !m.expanded[n.ID]
	m.rebuild()
}

// setExpand explicitly expands or collapses the selected container.
func (m *Model) setExpand(open bool) {
	n := m.selectedNode()
	if n == nil || len(n.Children) == 0 {
		return
	}
	m.expanded[n.ID] = open
	m.rebuild()
}

// clampScroll keeps the cursor within the visible tree window.
func (m *Model) clampScroll() {
	th := m.treeHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+th {
		m.offset = m.cursor - th + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if maxOff := len(m.rows) - th; m.offset > maxOff && maxOff >= 0 {
		m.offset = maxOff
	}
}
