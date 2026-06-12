package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// fakeAdapter is a scriptable adapter for runner tests.
type fakeAdapter struct {
	name        string
	emitOnRun   []event.Event
	gate        chan struct{} // if set, Run blocks until closed or ctx is done
	discoverErr error         // if set, Discover returns this without emitting

	mu          sync.Mutex
	runs        int
	lastSel     adapter.Selection
	ctxCanceled bool
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Detect(string) (adapter.Detection, bool) {
	return adapter.Detection{}, false
}
func (f *fakeAdapter) Discover(_ context.Context, _ string, emit adapter.Emit) error {
	if f.discoverErr != nil {
		return f.discoverErr
	}
	emit(event.NodeDiscovered(f.name+":p::T", "", "T", event.KindTest, nil))
	return nil
}
func (f *fakeAdapter) Run(ctx context.Context, _ string, sel adapter.Selection, emit adapter.Emit) error {
	f.mu.Lock()
	f.runs++
	f.lastSel = sel
	f.mu.Unlock()
	for _, e := range f.emitOnRun {
		emit(e)
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			f.mu.Lock()
			f.ctxCanceled = true
			f.mu.Unlock()
		}
	}
	return nil
}
func (f *fakeAdapter) Locate(string) (event.Location, bool) { return event.Location{}, false }

func detected(a adapter.Adapter) []adapter.Detected {
	return []adapter.Detected{{Adapter: a, Detection: adapter.Detection{RootDir: "/x"}}}
}

// waitFor reads events until one of the given type arrives or the test times out,
// returning all events seen up to and including it.
func waitFor(t *testing.T, sub *engine.Subscription, typ event.Type) []event.Event {
	t.Helper()
	var got []event.Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-sub.C:
			got = append(got, e)
			if e.Type == typ {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; saw %v", typ, types(got))
		}
	}
}

func types(evs []event.Event) []event.Type {
	out := make([]event.Type, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func TestDiscoverPopulatesEngine(t *testing.T) {
	eng := engine.New()
	r := New(context.Background(), eng, detected(&fakeAdapter{name: "go"}))
	if err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.Get("go:p::T") == nil {
		t.Fatal("Discover did not populate the engine")
	}
}

func TestDiscoverContinuesPastFailingAdapter(t *testing.T) {
	eng := engine.New()
	good := &fakeAdapter{name: "go"}
	bad := &fakeAdapter{name: "pytest", discoverErr: errors.New("python3: No module named pytest")}
	targets := []adapter.Detected{
		{Adapter: good, Detection: adapter.Detection{RootDir: "/x"}},
		{Adapter: bad, Detection: adapter.Detection{RootDir: "/y"}},
	}
	r := New(context.Background(), eng, targets)

	if err := r.Discover(context.Background()); err != nil {
		t.Fatalf("Discover should succeed when one adapter works, got %v", err)
	}
	if eng.Get("go:p::T") == nil {
		t.Error("working adapter's nodes were lost when another failed")
	}
	diag := eng.Get("!diag:pytest")
	if diag == nil || diag.Status != event.StatusError {
		t.Fatalf("expected an error diagnostic node for the failed adapter, got %v", diag)
	}
	if diag.Failure == nil || !strings.Contains(diag.Failure.Message, "No module named pytest") {
		t.Errorf("diagnostic should carry the adapter error, got %+v", diag.Failure)
	}
}

func TestDiscoverAllFailedReturnsError(t *testing.T) {
	eng := engine.New()
	bad := &fakeAdapter{name: "pytest", discoverErr: errors.New("boom")}
	r := New(context.Background(), eng, []adapter.Detected{
		{Adapter: bad, Detection: adapter.Detection{RootDir: "/y"}},
	})
	if err := r.Discover(context.Background()); err == nil {
		t.Error("Discover should return an error when every adapter fails")
	}
}

func TestRunAllBracketsWithRunStartedAndFinished(t *testing.T) {
	eng := engine.New()
	fa := &fakeAdapter{
		name: "go",
		emitOnRun: []event.Event{
			event.NodeDiscovered("go:p::T", "", "T", event.KindTest, nil),
			event.NodeFinished("go:p::T", event.StatusPass, 1, nil),
		},
	}
	r := New(context.Background(), eng, detected(fa))
	sub := eng.Subscribe(64)
	defer sub.Close()

	r.Run(engine.Selection{All: true})
	got := waitFor(t, sub, event.TypeRunFinished)

	if got[0].Type != event.TypeRunStarted {
		t.Fatalf("first event = %s, want run_started", got[0].Type)
	}
	if got[0].Scope != "all" {
		t.Fatalf("scope = %q, want all", got[0].Scope)
	}
	// All-selection passes an empty adapter selection (run everything).
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if fa.runs != 1 || len(fa.lastSel.NodeIDs) != 0 {
		t.Fatalf("adapter runs=%d sel=%v, want 1 run with empty selection", fa.runs, fa.lastSel)
	}
}

func TestRunSelectionRoutesNodeIDsToAdapter(t *testing.T) {
	eng := engine.New()
	fa := &fakeAdapter{name: "go"}
	r := New(context.Background(), eng, detected(fa))
	if err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	sub := eng.Subscribe(64)
	defer sub.Close()

	r.Run(engine.Selection{Nodes: []string{"go:p::T"}})
	waitFor(t, sub, event.TypeRunFinished)

	fa.mu.Lock()
	defer fa.mu.Unlock()
	if len(fa.lastSel.NodeIDs) != 1 || fa.lastSel.NodeIDs[0] != "go:p::T" {
		t.Fatalf("adapter selection = %v, want [go:p::T]", fa.lastSel.NodeIDs)
	}
}

func TestOverlappingRunIsIgnored(t *testing.T) {
	eng := engine.New()
	fa := &fakeAdapter{name: "go", gate: make(chan struct{})}
	r := New(context.Background(), eng, detected(fa))

	r.Run(engine.Selection{All: true}) // blocks in adapter until gate closes
	// Give the goroutine a moment to enter the adapter.
	waitUntil(t, func() bool { fa.mu.Lock(); defer fa.mu.Unlock(); return fa.runs == 1 })

	r.Run(engine.Selection{All: true}) // should be ignored while running
	close(fa.gate)

	waitUntil(t, func() bool { return !r.isRunning() })
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if fa.runs != 1 {
		t.Fatalf("adapter runs = %d, want 1 (overlapping run should be ignored)", fa.runs)
	}
}

func TestCancelStopsRun(t *testing.T) {
	eng := engine.New()
	fa := &fakeAdapter{name: "go", gate: make(chan struct{})}
	r := New(context.Background(), eng, detected(fa))

	r.Run(engine.Selection{All: true})
	waitUntil(t, func() bool { fa.mu.Lock(); defer fa.mu.Unlock(); return fa.runs == 1 })
	r.Cancel()

	waitUntil(t, func() bool { fa.mu.Lock(); defer fa.mu.Unlock(); return fa.ctxCanceled })
}

// isRunning exposes run state for tests.
func (r *Runner) isRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

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
