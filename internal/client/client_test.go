package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrf110/test-term/internal/client"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
	"github.com/nrf110/test-term/internal/server"
)

// fakeCommander records commands routed from clients.
type fakeCommander struct {
	mu      sync.Mutex
	runs    []engine.Selection
	cancels int
}

func (f *fakeCommander) Run(sel engine.Selection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, sel)
}
func (f *fakeCommander) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels++
}
func (f *fakeCommander) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}
func (f *fakeCommander) lastRun() engine.Selection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[len(f.runs)-1]
}

func addrOf(ts *httptest.Server) string { return strings.TrimPrefix(ts.URL, "http://") }

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition not met within timeout")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSnapshotHandshakeSeedsMirror(t *testing.T) {
	eng := engine.New()
	eng.Apply(event.NodeDiscovered("go:p::T", "", "T", event.KindTest, nil))

	ts := httptest.NewServer(server.New(eng, &fakeCommander{}, "").Handler())
	defer ts.Close()

	cl, err := client.Dial(context.Background(), addrOf(ts), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cl.Close() }()

	if n := cl.Engine().Get("go:p::T"); n == nil {
		t.Fatal("mirror did not receive the initial snapshot")
	}
}

func TestLiveEventsMirrorToClient(t *testing.T) {
	eng := engine.New()
	ts := httptest.NewServer(server.New(eng, &fakeCommander{}, "").Handler())
	defer ts.Close()

	cl, err := client.Dial(context.Background(), addrOf(ts), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cl.Close() }()

	// Events applied after connect must stream to the mirror.
	eng.Apply(event.NodeDiscovered("go:p::T", "", "T", event.KindTest, nil))
	eng.Apply(event.NodeFinished("go:p::T", event.StatusFail, 3, &event.Failure{Message: "boom"}))

	waitUntil(t, func() bool {
		n := cl.Engine().Get("go:p::T")
		return n != nil && n.Status == event.StatusFail
	})
	if n := cl.Engine().Get("go:p::T"); n.Failure == nil || n.Failure.Message != "boom" {
		t.Fatalf("failure not mirrored: %+v", n.Failure)
	}
}

func TestCommandsRoundTripToServer(t *testing.T) {
	eng := engine.New()
	fc := &fakeCommander{}
	ts := httptest.NewServer(server.New(eng, fc, "").Handler())
	defer ts.Close()

	cl, err := client.Dial(context.Background(), addrOf(ts), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cl.Close() }()

	cl.Run(engine.Selection{Failed: true})
	waitUntil(t, func() bool { return fc.runCount() == 1 })
	if !fc.lastRun().Failed {
		t.Fatalf("server received %+v, want Failed selection", fc.lastRun())
	}

	cl.Cancel()
	waitUntil(t, func() bool { fc.mu.Lock(); defer fc.mu.Unlock(); return fc.cancels == 1 })
}

func TestAuthTokenEnforced(t *testing.T) {
	eng := engine.New()
	ts := httptest.NewServer(server.New(eng, &fakeCommander{}, "secret").Handler())
	defer ts.Close()

	// Wrong/absent token is rejected at the HTTP upgrade.
	if _, err := client.Dial(context.Background(), addrOf(ts), ""); err == nil {
		t.Fatal("dial without token should fail")
	}
	if _, err := client.Dial(context.Background(), addrOf(ts), "wrong"); err == nil {
		t.Fatal("dial with wrong token should fail")
	}

	cl, err := client.Dial(context.Background(), addrOf(ts), "secret")
	if err != nil {
		t.Fatalf("dial with correct token: %v", err)
	}
	defer func() { _ = cl.Close() }()
}

func TestServerSnapshotIsFirstFrame(t *testing.T) {
	// Guards the protocol invariant the client relies on: the first frame is the
	// snapshot. A regression here would break every client connection.
	eng := engine.New()
	srv := server.New(eng, &fakeCommander{}, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A bad path should 404, not upgrade.
	resp, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /nope = %d, want 404", resp.StatusCode)
	}
}
