package server_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nrf110/test-term/internal/client"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
	"github.com/nrf110/test-term/internal/server"
)

// fakeCommander finishes one test and emits RunFinished when Run is called.
type fakeCommander struct{ eng *engine.Session }

func (f *fakeCommander) Run(engine.Selection) {
	f.eng.Apply(event.RunStarted("r", "all"))
	f.eng.Apply(event.NodeFinished("go:p::T", event.StatusFail, 1, &event.Failure{Message: "boom"}))
	f.eng.Apply(event.RunFinished("r", f.eng.Summary()))
}
func (f *fakeCommander) Cancel() {}

func addrOf(ts *httptest.Server) string { return strings.TrimPrefix(ts.URL, "http://") }

func newEngine() *engine.Session {
	eng := engine.New()
	eng.Apply(event.NodeDiscovered("go:p", "", "p", event.KindFile, nil))
	eng.Apply(event.NodeDiscovered("go:p::T", "go:p", "T", event.KindTest, nil))
	return eng
}

func dialMCP(t *testing.T, ts *httptest.Server) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	sess, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect mcp: %v", err)
	}
	return sess
}

func TestMCPListToolsAndCall(t *testing.T) {
	eng := newEngine()
	ts := httptest.NewServer(server.New(eng, &fakeCommander{eng: eng}, "").Handler())
	defer ts.Close()

	sess := dialMCP(t, ts)
	defer func() { _ = sess.Close() }()

	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{
		"list_tests": false, "run_tests": false, "rerun_failed": false,
		"get_results": false, "get_failure": false, "cancel_run": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not advertised", name)
		}
	}

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_tests",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call list_tests: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_tests returned error result: %+v", res.Content)
	}
	if res.StructuredContent == nil {
		t.Fatal("list_tests produced no structured content")
	}
}

func TestMCPRunObservedByWSClient(t *testing.T) {
	// The shared-session guarantee: a run triggered over MCP is observed live by
	// a separate WebSocket client connected to the same engine.
	eng := newEngine()
	ts := httptest.NewServer(server.New(eng, &fakeCommander{eng: eng}, "").Handler())
	defer ts.Close()

	wsClient, err := client.Dial(context.Background(), addrOf(ts), "")
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = wsClient.Close() }()

	sess := dialMCP(t, ts)
	defer func() { _ = sess.Close() }()

	if _, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "run_tests",
		Arguments: map[string]any{"all": true},
	}); err != nil {
		t.Fatalf("call run_tests: %v", err)
	}

	// The WS client's mirror must reflect the MCP-triggered run.
	deadline := time.After(2 * time.Second)
	for {
		if n := wsClient.Engine().Get("go:p::T"); n != nil && n.Status == event.StatusFail {
			return
		}
		select {
		case <-deadline:
			t.Fatal("WS client did not observe the MCP-triggered run")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
