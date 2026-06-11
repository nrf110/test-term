package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// formatLoc renders a source location as file:line:col, omitting empty parts.
// This is the form terminals like Warp detect and make clickable.
func formatLoc(loc event.Location) string {
	if loc.File == "" {
		return ""
	}
	s := loc.File
	if loc.Line > 0 {
		s += ":" + strconv.Itoa(loc.Line)
		if loc.Col > 0 {
			s += ":" + strconv.Itoa(loc.Col)
		}
	}
	return s
}

// primaryLocation returns the most relevant location for a node: the top failure
// frame if it failed, otherwise its declared location.
func primaryLocation(n *engine.Node) (event.Location, bool) {
	if n.Failure != nil && len(n.Failure.Frames) > 0 {
		f := n.Failure.Frames[0]
		return event.Location{File: f.File, Line: f.Line, Col: f.Col}, true
	}
	if n.Location != nil {
		return *n.Location, true
	}
	return event.Location{}, false
}

// renderDetail produces the body of the detail pane for the selected node.
func renderDetail(n *engine.Node) string {
	if n == nil {
		return styleDim.Render("No test selected.")
	}

	var b strings.Builder
	title := statusStyle(n.Status).Render(glyph(n.Status) + " " + n.Name)
	b.WriteString(title)
	if loc, ok := primaryLocation(n); ok {
		if s := formatLoc(loc); s != "" {
			b.WriteString("  " + styleDim.Render(s))
		}
	}
	b.WriteString("\n")

	switch {
	case n.Status == event.StatusRunning:
		b.WriteString(styleRunning.Render("running…"))
	case n.Failure != nil:
		b.WriteString(n.Failure.Message)
	case n.Status == event.StatusPass:
		b.WriteString(styleDim.Render(fmt.Sprintf("passed in %s", duration(n.DurationMs))))
	case n.Status == event.StatusSkip:
		b.WriteString(styleDim.Render("skipped"))
	default:
		b.WriteString(styleDim.Render("not run yet"))
	}
	return b.String()
}

// duration renders milliseconds compactly (e.g. "12ms", "1.80s").
func duration(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}
