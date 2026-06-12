package tui

import "github.com/nrf110/test-term/internal/engine"

// detailLines is the preferred height of the detail region (title + message
// body). It shrinks on short terminals via layout.
const detailLines = 6

// layout distributes the terminal height across the tree and detail regions,
// accounting for fixed chrome (header + two separators + footer = 4 lines). On
// short terminals the detail region shrinks first, then the tree, so the UI
// never overflows for any height >= ~6.
func (m Model) layout() (treeH, detailH int) {
	detailH = detailLines
	if maxDetail := m.height - 6; detailH > maxDetail {
		detailH = maxDetail
	}
	if detailH < 1 {
		detailH = 1
	}
	treeH = m.height - detailH - 4
	if treeH < 1 {
		treeH = 1
	}
	return treeH, detailH
}

// treeHeight is the number of visible tree rows for the current size.
func (m Model) treeHeight() int {
	th, _ := m.layout()
	return th
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
