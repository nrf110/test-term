package client_test

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/adapter/gotest"
	"github.com/nrf110/test-term/internal/client"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
	"github.com/nrf110/test-term/internal/runner"
	"github.com/nrf110/test-term/internal/server"
)

// TestEndToEndRunThroughServer exercises the whole local path: a real runner +
// Go adapter feeds the engine, the server streams to a client, and a run
// triggered over the wire produces results in the client's mirror.
func TestEndToEndRunThroughServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	dir, err := filepath.Abs(filepath.Join("..", "adapter", "gotest", "testdata", "projects", "sample"))
	if err != nil {
		t.Fatal(err)
	}

	eng := engine.New()
	run := runner.New(context.Background(), eng, []adapter.Detected{
		{Adapter: gotest.New(), Detection: adapter.Detection{RootDir: dir}},
	})
	if err := run.Discover(context.Background()); err != nil {
		t.Fatalf("discover: %v", err)
	}

	ts := httptest.NewServer(server.New(eng, run, "").Handler())
	defer ts.Close()

	cl, err := client.Dial(context.Background(), addrOf(ts), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cl.Close() }()

	// The discovered tree is in the snapshot.
	if cl.Engine().Get("go:example.com/sample::TestAdd") == nil {
		t.Fatal("discovered test missing from client snapshot")
	}

	// Trigger a full run over the wire; results stream back to the mirror.
	cl.Run(engine.Selection{All: true})
	waitUntil(t, func() bool {
		n := cl.Engine().Get("go:example.com/sample::TestAdd")
		return n != nil && n.Status == event.StatusPass
	})
	if n := cl.Engine().Get("go:example.com/sample::TestAlwaysFails"); n == nil || n.Status != event.StatusFail {
		t.Fatalf("failing test mirrored as %v, want fail", n)
	}
}
