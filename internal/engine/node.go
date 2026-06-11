package engine

import "github.com/nrf110/test-term/internal/event"

// Node is one entry in the test tree. Container nodes (files, suites) derive
// their Status and DurationMs from their children; leaf nodes (tests) carry the
// status set directly by NodeStarted/NodeFinished events.
type Node struct {
	ID         string          `json:"id"`
	ParentID   string          `json:"parentId,omitempty"`
	Name       string          `json:"name"`
	Kind       event.Kind      `json:"kind"`
	Status     event.Status    `json:"status"`
	DurationMs int64           `json:"durationMs"`
	Location   *event.Location `json:"location,omitempty"`
	Failure    *event.Failure  `json:"failure,omitempty"`
	Children   []*Node         `json:"children,omitempty"`
}

// IsLeaf reports whether the node has no children. Leaves are the runnable test
// cases; their status is authoritative. Containers aggregate their descendants.
func (n *Node) IsLeaf() bool { return len(n.Children) == 0 }

// clone returns a deep copy of the node and its subtree, so snapshots handed to
// subscribers cannot be mutated by — or mutate — the live tree.
func (n *Node) clone() *Node {
	cp := *n
	if n.Location != nil {
		loc := *n.Location
		cp.Location = &loc
	}
	if n.Failure != nil {
		f := *n.Failure
		if n.Failure.Frames != nil {
			f.Frames = append([]event.Frame(nil), n.Failure.Frames...)
		}
		cp.Failure = &f
	}
	if len(n.Children) > 0 {
		cp.Children = make([]*Node, len(n.Children))
		for i, c := range n.Children {
			cp.Children[i] = c.clone()
		}
	}
	return &cp
}

// aggregateStatus derives a container's status from its children.
//
// Precedence is tuned for how a test explorer should read at a glance:
//   - running, if any child is in flight (there is active work);
//   - else error, then fail, if anything failed;
//   - else pass, if at least one child passed;
//   - else skip, if everything was skipped;
//   - else pending.
func aggregateStatus(children []*Node) event.Status {
	var hasRunning, hasError, hasFail, hasPass, hasSkip, hasPending bool
	for _, c := range children {
		switch c.Status {
		case event.StatusRunning:
			hasRunning = true
		case event.StatusError:
			hasError = true
		case event.StatusFail:
			hasFail = true
		case event.StatusPass:
			hasPass = true
		case event.StatusSkip:
			hasSkip = true
		default:
			hasPending = true
		}
	}
	switch {
	case hasRunning:
		return event.StatusRunning
	case hasError:
		return event.StatusError
	case hasFail:
		return event.StatusFail
	case hasPass:
		return event.StatusPass
	case hasSkip:
		return event.StatusSkip
	case hasPending:
		return event.StatusPending
	default:
		return event.StatusPending
	}
}
