// Package event defines the normalized vocabulary that every adapter emits and
// that every client (TUI, MCP) consumes. Nothing outside an adapter knows about
// a specific test framework — the engine, server, and UI speak only in these
// events.
//
// Events are carried in a single Event envelope with a Type discriminator and a
// flat set of optional fields. This keeps JSON (de)serialization trivial and
// language-agnostic (durations are integer milliseconds, statuses are strings),
// which matters because the same JSON crosses the wire to non-Go clients via the
// WebSocket protocol and MCP. Constructors below keep call sites intent-revealing
// despite the shared shape.
package event

// Type is the event discriminator.
type Type string

const (
	TypeRunStarted     Type = "run_started"
	TypeNodeDiscovered Type = "node_discovered"
	TypeNodeStarted    Type = "node_started"
	TypeNodeFinished   Type = "node_finished"
	TypeOutput         Type = "output"
	TypeRunFinished    Type = "run_finished"
)

// Kind classifies a node in the test tree.
type Kind string

const (
	KindFile  Kind = "file"  // a test file or package
	KindSuite Kind = "suite" // a describe/class/group within a file
	KindTest  Kind = "test"  // a leaf test case
)

// Status is the state of a node.
type Status string

const (
	StatusPending Status = "pending" // discovered, not yet run
	StatusRunning Status = "running" // currently executing
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusSkip    Status = "skip"
	StatusError   Status = "error" // errored/panicked rather than asserted-failed
)

// Stream identifies a captured output stream.
type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

// Location is a source position used for jump-to-failure and node metadata.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// Frame is a single stack frame in a failure.
type Frame struct {
	Function string `json:"function,omitempty"`
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
}

// Failure carries the detail shown for a failed/errored node.
type Failure struct {
	Message string  `json:"message"`
	Diff    string  `json:"diff,omitempty"`
	Frames  []Frame `json:"frames,omitempty"`
}

// Summary aggregates a finished run. It is computable from the tree, but is
// carried on RunFinished so clients need not walk the tree to show totals.
type Summary struct {
	Total      int   `json:"total"`
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Skipped    int   `json:"skipped"`
	Errored    int   `json:"errored"`
	DurationMs int64 `json:"durationMs"`
}

// Event is the envelope carrying one event of a given Type. Only the fields
// relevant to that Type are populated; constructors enforce correct usage.
type Event struct {
	Type Type `json:"type"`

	// Run-scoped.
	RunID   string   `json:"runId,omitempty"`
	Scope   string   `json:"scope,omitempty"`
	Summary *Summary `json:"summary,omitempty"`

	// Node-scoped.
	NodeID     string    `json:"nodeId,omitempty"`
	ParentID   string    `json:"parentId,omitempty"`
	Name       string    `json:"name,omitempty"`
	NodeKind   Kind      `json:"kind,omitempty"`
	Status     Status    `json:"status,omitempty"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Location   *Location `json:"location,omitempty"`
	Failure    *Failure  `json:"failure,omitempty"`

	// Output-scoped.
	Stream Stream `json:"stream,omitempty"`
	Text   string `json:"text,omitempty"`
}

// RunStarted reports the beginning of a run. scope is a human-readable
// description of what is being run (e.g. "all", "failed", a file path).
func RunStarted(runID, scope string) Event {
	return Event{Type: TypeRunStarted, RunID: runID, Scope: scope}
}

// NodeDiscovered announces a node in the tree. It may arrive before any run
// (static discovery) or mid-run (runtime-created subtests / parametrized cases).
// parentID is empty for a root node. loc may be nil.
func NodeDiscovered(nodeID, parentID, name string, kind Kind, loc *Location) Event {
	return Event{
		Type:     TypeNodeDiscovered,
		NodeID:   nodeID,
		ParentID: parentID,
		Name:     name,
		NodeKind: kind,
		Location: loc,
	}
}

// NodeStarted marks a node as currently executing.
func NodeStarted(nodeID string) Event {
	return Event{Type: TypeNodeStarted, NodeID: nodeID}
}

// NodeFinished records a node's terminal status, duration, and (if failing) its
// failure detail. failure may be nil for non-failing statuses.
func NodeFinished(nodeID string, status Status, durationMs int64, failure *Failure) Event {
	return Event{
		Type:       TypeNodeFinished,
		NodeID:     nodeID,
		Status:     status,
		DurationMs: durationMs,
		Failure:    failure,
	}
}

// Output carries captured output. nodeID may be empty for run-level output.
func Output(nodeID string, stream Stream, text string) Event {
	return Event{Type: TypeOutput, NodeID: nodeID, Stream: stream, Text: text}
}

// RunFinished reports the end of a run with its summary.
func RunFinished(runID string, summary Summary) Event {
	return Event{Type: TypeRunFinished, RunID: runID, Summary: &summary}
}

// Terminal reports whether a status represents a completed (non-in-flight) state.
func (s Status) Terminal() bool {
	switch s {
	case StatusPass, StatusFail, StatusSkip, StatusError:
		return true
	default:
		return false
	}
}

// Failing reports whether a status represents a failing outcome.
func (s Status) Failing() bool {
	return s == StatusFail || s == StatusError
}
