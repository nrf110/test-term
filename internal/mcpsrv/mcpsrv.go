// Package mcpsrv exposes the engine to AI agents over MCP. The tools are thin
// wrappers over the same engine and runner the TUI uses, so an agent and a
// connected TUI drive and observe one shared session.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// Commander runs and cancels test runs (satisfied by *runner.Runner). Declared
// here to avoid an import cycle with the server package.
type Commander interface {
	Run(sel engine.Selection)
	Cancel()
}

// tools holds the dependencies the MCP tool handlers close over.
type tools struct {
	eng  *engine.Session
	ctrl Commander
}

// NewServer builds an MCP server exposing the test-running tools.
func NewServer(eng *engine.Session, ctrl Commander) *mcp.Server {
	t := &tools{eng: eng, ctrl: ctrl}
	s := mcp.NewServer(&mcp.Implementation{Name: "tt", Version: "v0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_tests",
		Description: "List the discovered test tree with current statuses.",
	}, t.listTests)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "run_tests",
		Description: "Run tests (all, previously failed, or specific node IDs) and wait for the result. Defaults to all when no selection is given.",
	}, t.runTests)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "rerun_failed",
		Description: "Re-run only the tests that are currently failing, and wait for the result.",
	}, t.rerunFailed)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_results",
		Description: "Get the current run summary and the list of failing tests.",
	}, t.getResults)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_failure",
		Description: "Get the failure detail (message, diff, stack frames, location) for a node.",
	}, t.getFailure)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "cancel_run",
		Description: "Cancel the test run currently in progress, if any.",
	}, t.cancelRun)

	return s
}

// --- Input/Output types ---------------------------------------------------

// noInput is an empty object schema for tools that take no arguments.
type noInput struct{}

// Failure is a flattened failing leaf, convenient for agents.
type Failure struct {
	NodeID  string       `json:"nodeId"`
	Name    string       `json:"name"`
	Status  event.Status `json:"status"`
	Message string       `json:"message,omitempty"`
	File    string       `json:"file,omitempty"`
	Line    int          `json:"line,omitempty"`
	Col     int          `json:"col,omitempty"`
}

// TestNode is a flattened tree node. The tree is conveyed as a flat list keyed
// by ParentID (empty for roots) rather than a nested structure — both because a
// recursive type has no inferable JSON schema and because a flat list is simpler
// for an agent to scan.
type TestNode struct {
	ID         string       `json:"id"`
	ParentID   string       `json:"parentId,omitempty"`
	Name       string       `json:"name"`
	Kind       event.Kind   `json:"kind"`
	Status     event.Status `json:"status"`
	DurationMs int64        `json:"durationMs,omitempty"`
	File       string       `json:"file,omitempty"`
	Line       int          `json:"line,omitempty"`
}

type ListOutput struct {
	Tests   []TestNode    `json:"tests"`
	Summary event.Summary `json:"summary"`
}

type RunInput struct {
	All    bool     `json:"all,omitempty" jsonschema:"run every test"`
	Failed bool     `json:"failed,omitempty" jsonschema:"run only currently-failing tests"`
	Nodes  []string `json:"nodes,omitempty" jsonschema:"specific node IDs to run (containers expand to their tests)"`
}

type RunOutput struct {
	Summary  event.Summary `json:"summary"`
	Failures []Failure     `json:"failures"`
}

type ResultsOutput struct {
	Summary  event.Summary `json:"summary"`
	Failures []Failure     `json:"failures"`
}

type FailureInput struct {
	NodeID string `json:"nodeId" jsonschema:"the node ID to fetch failure detail for"`
}

type FailureOutput struct {
	Found   bool           `json:"found"`
	NodeID  string         `json:"nodeId"`
	Name    string         `json:"name,omitempty"`
	Status  event.Status   `json:"status,omitempty"`
	Failure *event.Failure `json:"failure,omitempty"`
}

type CancelOutput struct {
	Canceled bool `json:"canceled"`
}

// --- Handlers -------------------------------------------------------------

func (t *tools) listTests(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, ListOutput, error) {
	var flat []TestNode
	var walk func(n *engine.Node)
	walk = func(n *engine.Node) {
		tn := TestNode{
			ID: n.ID, ParentID: n.ParentID, Name: n.Name,
			Kind: n.Kind, Status: n.Status, DurationMs: n.DurationMs,
		}
		if n.Location != nil {
			tn.File, tn.Line = n.Location.File, n.Location.Line
		}
		flat = append(flat, tn)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.eng.Snapshot() {
		walk(r)
	}
	return nil, ListOutput{Tests: flat, Summary: t.eng.Summary()}, nil
}

func (t *tools) runTests(ctx context.Context, _ *mcp.CallToolRequest, in RunInput) (*mcp.CallToolResult, RunOutput, error) {
	sel := engine.Selection{All: in.All, Failed: in.Failed, Nodes: in.Nodes}
	if !sel.All && !sel.Failed && len(sel.Nodes) == 0 {
		sel.All = true // sensible default
	}
	summary := t.runAndWait(ctx, sel)
	return nil, RunOutput{Summary: summary, Failures: t.failures()}, nil
}

func (t *tools) rerunFailed(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, RunOutput, error) {
	summary := t.runAndWait(ctx, engine.Selection{Failed: true})
	return nil, RunOutput{Summary: summary, Failures: t.failures()}, nil
}

func (t *tools) getResults(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, ResultsOutput, error) {
	return nil, ResultsOutput{Summary: t.eng.Summary(), Failures: t.failures()}, nil
}

func (t *tools) getFailure(_ context.Context, _ *mcp.CallToolRequest, in FailureInput) (*mcp.CallToolResult, FailureOutput, error) {
	n := t.eng.Get(in.NodeID)
	if n == nil {
		return nil, FailureOutput{Found: false, NodeID: in.NodeID}, nil
	}
	return nil, FailureOutput{
		Found:   true,
		NodeID:  n.ID,
		Name:    n.Name,
		Status:  n.Status,
		Failure: n.Failure,
	}, nil
}

func (t *tools) cancelRun(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, CancelOutput, error) {
	t.ctrl.Cancel()
	return nil, CancelOutput{Canceled: true}, nil
}

// --- Helpers --------------------------------------------------------------

// runAndWait triggers a run and blocks until it finishes (or ctx ends),
// returning the summary. Subscribing before issuing the command guarantees the
// terminating RunFinished is observed even if the run is brief.
func (t *tools) runAndWait(ctx context.Context, sel engine.Selection) event.Summary {
	sub := t.eng.Subscribe(256)
	defer sub.Close()

	t.ctrl.Run(sel)
	for {
		select {
		case e := <-sub.C:
			if e.Type == event.TypeRunFinished && e.Summary != nil {
				return *e.Summary
			}
		case <-ctx.Done():
			return t.eng.Summary()
		case <-sub.Done():
			return t.eng.Summary()
		}
	}
}

// failures walks the current tree and returns every failing leaf, flattened.
func (t *tools) failures() []Failure {
	var out []Failure
	var walk func(n *engine.Node)
	walk = func(n *engine.Node) {
		if len(n.Children) == 0 {
			if n.Status.Failing() {
				out = append(out, toFailure(n))
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.eng.Snapshot() {
		walk(r)
	}
	return out
}

func toFailure(n *engine.Node) Failure {
	f := Failure{NodeID: n.ID, Name: n.Name, Status: n.Status}
	if n.Failure != nil {
		f.Message = n.Failure.Message
		if len(n.Failure.Frames) > 0 {
			fr := n.Failure.Frames[0]
			f.File, f.Line, f.Col = fr.File, fr.Line, fr.Col
		}
	}
	return f
}
