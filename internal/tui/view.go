package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View renders the full layout (B: header, tree, detail, footer stacked).
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.renderHelpScreen()
	}

	th := m.treeHeight()
	sep := styleBar.Render(strings.Repeat("─", m.width))
	return strings.Join([]string{
		m.renderHeader(),
		sep,
		m.renderTreeRegion(th),
		sep,
		m.renderDetailRegion(detailLines),
		m.renderFooter(),
	}, "\n")
}

func (m Model) renderHeader() string {
	left := styleBold.Render("tt") + " ▸ " + m.project + "   " + styleDim.Render("● "+m.conn)

	sum := m.session.Summary()
	counts := strings.Join([]string{
		stylePass.Render("✓" + strconv.Itoa(sum.Passed)),
		styleFail.Render("✗" + strconv.Itoa(sum.Failed+sum.Errored)),
		styleSkip.Render("⊘" + strconv.Itoa(sum.Skipped)),
	}, " ")
	right := counts + "  " + styleDim.Render(duration(sum.DurationMs))
	if m.running {
		right = styleRunning.Render("⟳ running") + "  " + right
	}
	return pad(left, right, m.width)
}

func (m Model) renderTreeRegion(height int) string {
	lines := make([]string, 0, height)
	for i := m.offset; i < m.offset+height && i < len(m.rows); i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == m.cursor))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderRow(r row, selected bool) string {
	indent := strings.Repeat("  ", r.depth)
	caret := "  "
	if r.hasChildren() {
		if m.expanded[r.node.ID] || m.filter.Value() != "" {
			caret = "▼ "
		} else {
			caret = "▶ "
		}
	}
	g := statusStyle(r.node.Status).Render(glyph(r.node.Status))
	line := indent + caret + g + " " + r.node.Name
	if r.node.DurationMs > 0 && r.node.Status.Terminal() {
		line += "  " + styleDim.Render(duration(r.node.DurationMs))
	}

	line = ansi.Truncate(line, m.width, "…")
	if selected {
		return styleSelected.Width(m.width).Render(line)
	}
	return line
}

func (m Model) renderDetailRegion(height int) string {
	content := renderDetail(m.selectedNode())
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "…")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderFooter() string {
	if m.filtering {
		return m.filter.View()
	}
	if m.notice != "" {
		return styleDim.Render(m.notice)
	}
	return m.help.ShortHelpView(m.keys.ShortHelp())
}

func (m Model) renderHelpScreen() string {
	var b strings.Builder
	b.WriteString(styleBold.Render("tt — keybindings") + "\n\n")
	for _, col := range m.keys.FullHelp() {
		for _, bind := range col {
			h := bind.Help()
			b.WriteString("  " + styleBold.Render(padRight(h.Key, 8)) + " " + h.Desc + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(styleDim.Render("press ? or esc to return"))
	body := b.String()
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// pad places left and right text on a single line of the given width, filling
// the gap with spaces. If the combined text overflows, the line is truncated.
func pad(left, right string, width int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	gap := width - lw - rw
	if gap < 1 {
		return ansi.Truncate(left+" "+right, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func padRight(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
