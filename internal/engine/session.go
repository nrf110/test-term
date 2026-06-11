// Package engine holds the single source of truth for a test session: the tree
// of discovered/running tests built from a stream of normalized events, plus the
// fan-out that lets multiple clients (local TUI, remote TUI, MCP agent) observe
// the same state. The engine has no knowledge of frameworks, transports, or UI.
package engine

import (
	"sort"
	"sync"

	"github.com/nrf110/test-term/internal/event"
)

// Session is the live test tree. It is safe for concurrent use: Apply mutates
// under a write lock and snapshots/queries take a read lock.
type Session struct {
	mu    sync.RWMutex
	roots []*Node
	index map[string]*Node // nodeID -> node, for O(1) lookup
	runID string

	// lastSummary is the summary from the most recent RunFinished, if any.
	lastSummary event.Summary
	hasSummary  bool

	// subscribers receive every applied event after the tree is updated.
	subs   map[int]*subscriber
	nextID int
}

// subscriber is one observer's delivery channel plus a done signal. The data
// channel is never closed; Close signals done instead, so a fan-out send racing
// with an unsubscribe can never panic on a closed channel.
type subscriber struct {
	ch   chan event.Event
	done chan struct{}
	once sync.Once
}

// send delivers e, or returns immediately if the subscriber has been closed.
// It blocks while the buffer is full (backpressure) unless/until done fires.
func (sb *subscriber) send(e event.Event) {
	select {
	case sb.ch <- e:
	case <-sb.done:
	}
}

func (sb *subscriber) close() { sb.once.Do(func() { close(sb.done) }) }

// New returns an empty session.
func New() *Session {
	return &Session{index: make(map[string]*Node), subs: make(map[int]*subscriber)}
}

// Apply updates the tree from a single event and fans the event out to all
// subscribers. Fan-out happens after the lock is released so a slow subscriber
// cannot stall tree mutation; subscriber channels are buffered to absorb bursts.
func (s *Session) Apply(e event.Event) {
	s.mu.Lock()
	s.mutate(e)
	targets := make([]*subscriber, 0, len(s.subs))
	for _, sb := range s.subs {
		targets = append(targets, sb)
	}
	s.mu.Unlock()

	for _, sb := range targets {
		sb.send(e)
	}
}

// mutate applies an event to the tree. Caller must hold the write lock.
func (s *Session) mutate(e event.Event) {
	switch e.Type {
	case event.TypeRunStarted:
		s.runID = e.RunID
		s.hasSummary = false

	case event.TypeNodeDiscovered:
		s.upsertNode(e)

	case event.TypeNodeStarted:
		if n := s.index[e.NodeID]; n != nil && n.IsLeaf() {
			n.Status = event.StatusRunning
			s.recomputeAncestors(n)
		}

	case event.TypeNodeFinished:
		if n := s.index[e.NodeID]; n != nil {
			n.Status = e.Status
			n.DurationMs = e.DurationMs
			n.Failure = e.Failure
			s.recomputeAncestors(n)
		}

	case event.TypeRunFinished:
		if e.Summary != nil {
			s.lastSummary = *e.Summary
			s.hasSummary = true
		}

	case event.TypeOutput:
		// Output is streamed to subscribers but not stored on the tree in v1.
	}
}

// upsertNode creates or updates a node from a NodeDiscovered event, attaching it
// under its parent (or as a root). Re-discovery is idempotent: it refreshes
// metadata without duplicating the node or losing children.
func (s *Session) upsertNode(e event.Event) {
	if n, ok := s.index[e.NodeID]; ok {
		n.Name = e.Name
		n.Kind = e.NodeKind
		if e.Location != nil {
			n.Location = e.Location
		}
		return
	}

	n := &Node{
		ID:       e.NodeID,
		ParentID: e.ParentID,
		Name:     e.Name,
		Kind:     e.NodeKind,
		Status:   event.StatusPending,
		Location: e.Location,
	}
	s.index[e.NodeID] = n

	if e.ParentID == "" {
		s.roots = append(s.roots, n)
		return
	}
	if parent := s.index[e.ParentID]; parent != nil {
		// A parent that gains children becomes a container; its status now
		// derives from them rather than any directly-set leaf status.
		parent.Children = append(parent.Children, n)
		s.recomputeAncestors(n)
	} else {
		// Parent not yet known; treat as a root for now. (Adapters in v1 emit
		// parents before children, so this is a defensive fallback.)
		s.roots = append(s.roots, n)
	}
}

// recomputeAncestors walks upward from a changed node, refreshing each
// container's aggregate status and summed duration.
func (s *Session) recomputeAncestors(n *Node) {
	for pid := n.ParentID; pid != ""; {
		p := s.index[pid]
		if p == nil {
			return
		}
		p.Status = aggregateStatus(p.Children)
		var sum int64
		for _, c := range p.Children {
			sum += c.DurationMs
		}
		p.DurationMs = sum
		pid = p.ParentID
	}
}

// Snapshot returns a deep copy of the current tree (root nodes). Callers may read
// and retain it freely; it will not change as the session evolves.
func (s *Session) Snapshot() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cloneRoots()
}

func (s *Session) cloneRoots() []*Node {
	out := make([]*Node, len(s.roots))
	for i, r := range s.roots {
		out[i] = r.clone()
	}
	return out
}

// Summary returns counts computed from the current leaf nodes. Unlike the
// RunFinished summary (a point-in-time wire convenience), this always reflects
// the live tree.
func (s *Session) Summary() event.Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sum event.Summary
	for _, r := range s.roots {
		walkLeaves(r, func(n *Node) {
			sum.Total++
			sum.DurationMs += n.DurationMs
			switch n.Status {
			case event.StatusPass:
				sum.Passed++
			case event.StatusFail:
				sum.Failed++
			case event.StatusSkip:
				sum.Skipped++
			case event.StatusError:
				sum.Errored++
			}
		})
	}
	return sum
}

func walkLeaves(n *Node, fn func(*Node)) {
	if n.IsLeaf() {
		fn(n)
		return
	}
	for _, c := range n.Children {
		walkLeaves(c, fn)
	}
}

// Subscription is a live feed of events for one observer.
type Subscription struct {
	// Snapshot is the tree as it stood when the subscription was created. Apply
	// the snapshot first, then the events from C.
	Snapshot []*Node
	// C delivers every event applied after the snapshot, in order. It is never
	// closed; use Done to detect teardown when ranging.
	C <-chan event.Event

	sub   *subscriber
	close func()
}

// Done is closed when the subscription is torn down. Consumers that block on C
// should also select on Done so they can exit cleanly.
func (sub *Subscription) Done() <-chan struct{} { return sub.sub.done }

// Close unsubscribes. It is idempotent; after it returns, no further events are
// delivered to this subscription.
func (sub *Subscription) Close() { sub.close() }

// Subscribe registers an observer and returns an atomic snapshot plus a live
// event channel. The snapshot and channel together give the subscriber a
// gap-free view: state at subscribe time, then every subsequent change.
func (s *Session) Subscribe(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = 256
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	sb := &subscriber{ch: make(chan event.Event, buffer), done: make(chan struct{})}
	s.subs[id] = sb

	return &Subscription{
		Snapshot: s.cloneRoots(),
		C:        sb.ch,
		sub:      sb,
		close: func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			sb.close()
		},
	}
}

// Selection describes which tests to run. Exactly one mode is intended at a
// time; All takes precedence, then Failed, then explicit Nodes.
type Selection struct {
	All    bool
	Failed bool
	Nodes  []string // node IDs (containers expand to their leaf descendants)
}

// Resolve expands a Selection into a deterministic, tree-ordered list of leaf
// node IDs. Unknown node IDs are ignored.
func (s *Session) Resolve(sel Selection) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []string
	switch {
	case sel.All:
		for _, r := range s.roots {
			walkLeaves(r, func(n *Node) { out = append(out, n.ID) })
		}
	case sel.Failed:
		for _, r := range s.roots {
			walkLeaves(r, func(n *Node) {
				if n.Status.Failing() {
					out = append(out, n.ID)
				}
			})
		}
	default:
		seen := make(map[string]bool)
		for _, id := range sel.Nodes {
			n := s.index[id]
			if n == nil {
				continue
			}
			walkLeaves(n, func(leaf *Node) {
				if !seen[leaf.ID] {
					seen[leaf.ID] = true
					out = append(out, leaf.ID)
				}
			})
		}
		// Explicit selections may name nodes across the tree out of order;
		// return them in stable tree order for predictable runs.
		out = s.orderByTree(out)
	}
	return out
}

// orderByTree returns ids sorted by their position in a depth-first walk.
func (s *Session) orderByTree(ids []string) []string {
	pos := make(map[string]int, len(ids))
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	i := 0
	for _, r := range s.roots {
		walkLeaves(r, func(n *Node) {
			if want[n.ID] {
				if _, ok := pos[n.ID]; !ok {
					pos[n.ID] = i
					i++
				}
			}
		})
	}
	sort.SliceStable(ids, func(a, b int) bool { return pos[ids[a]] < pos[ids[b]] })
	return ids
}

// Get returns a deep copy of a single node by ID, or nil if absent.
func (s *Session) Get(nodeID string) *Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n := s.index[nodeID]; n != nil {
		return n.clone()
	}
	return nil
}
