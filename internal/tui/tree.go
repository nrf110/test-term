package tui

import (
	"strings"

	"github.com/nrf110/test-term/internal/engine"
)

// row is one visible line in the tree: a node at a given indentation depth.
type row struct {
	node  *engine.Node
	depth int
}

// buildRows flattens the tree into the visible row list, honoring expansion and
// an optional filter. It is a pure function of its inputs so it can be tested
// without a running program.
//
// Filtering: a node is included if its name matches or any descendant matches,
// so the path to a match stays visible; while filtering, containers are treated
// as expanded so matches are never hidden behind a collapsed parent.
func buildRows(roots []*engine.Node, expanded map[string]bool, filter string) []row {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var out []row
	for _, r := range roots {
		rows, _ := rowsFor(r, 0, expanded, filter)
		out = append(out, rows...)
	}
	return out
}

func rowsFor(n *engine.Node, depth int, expanded map[string]bool, filter string) ([]row, bool) {
	selfMatch := filter == "" || strings.Contains(strings.ToLower(n.Name), filter)

	var childRows []row
	childMatch := false
	for _, c := range n.Children {
		rows, matched := rowsFor(c, depth+1, expanded, filter)
		if matched {
			childMatch = true
			childRows = append(childRows, rows...)
		}
	}

	if !selfMatch && !childMatch {
		return nil, false
	}

	out := []row{{node: n, depth: depth}}
	// Show children when expanded, or always while filtering (so matches show).
	if len(n.Children) > 0 && (filter != "" || expanded[n.ID]) {
		out = append(out, childRows...)
	}
	return out, true
}

// hasChildren reports whether a node is a container.
func (r row) hasChildren() bool { return len(r.node.Children) > 0 }
