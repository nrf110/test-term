// Package runner wires detected adapters to the engine. It owns run lifecycle:
// discovery, dispatching a Selection to the right adapters, bracketing each run
// with RunStarted/RunFinished, and cancellation. It implements tui.Controller.
package runner

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// Runner coordinates runs across the adapters detected for a project.
type Runner struct {
	eng     *engine.Session
	targets []adapter.Detected
	base    context.Context

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	seq     int
}

// New returns a Runner for the given engine and detected adapters. base scopes
// every run's lifetime (cancel it to stop the app's runs on shutdown).
func New(base context.Context, eng *engine.Session, targets []adapter.Detected) *Runner {
	return &Runner{eng: eng, targets: targets, base: base}
}

// Discover populates the engine's tree from every detected adapter. It runs
// synchronously and is intended to be called once before the UI starts.
func (r *Runner) Discover(ctx context.Context) error {
	for _, t := range r.targets {
		if err := t.Adapter.Discover(ctx, t.Detection.RootDir, r.eng.Apply); err != nil {
			return err
		}
	}
	return nil
}

// Run starts a run for the selection. It is non-blocking; if a run is already in
// flight the request is ignored. Results stream to the engine as events.
func (r *Runner) Run(sel engine.Selection) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.base)
	r.running = true
	r.cancel = cancel
	r.seq++
	runID := "run-" + strconv.Itoa(r.seq)
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.running = false
			r.cancel = nil
			r.mu.Unlock()
			cancel()
		}()
		r.eng.Apply(event.RunStarted(runID, scopeName(sel)))
		r.dispatch(ctx, sel)
		r.eng.Apply(event.RunFinished(runID, r.eng.Summary()))
	}()
}

// Cancel stops the in-flight run, if any.
func (r *Runner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

// dispatch routes a selection to the adapters that own the selected nodes.
func (r *Runner) dispatch(ctx context.Context, sel engine.Selection) {
	if sel.All {
		for _, t := range r.targets {
			_ = t.Adapter.Run(ctx, t.Detection.RootDir, adapter.Selection{}, r.eng.Apply)
		}
		return
	}

	ids := r.eng.Resolve(sel)
	if len(ids) == 0 {
		return
	}
	groups := make(map[string][]string)
	for _, id := range ids {
		for _, t := range r.targets {
			if strings.HasPrefix(id, t.Adapter.Name()+":") {
				groups[t.Adapter.Name()] = append(groups[t.Adapter.Name()], id)
				break
			}
		}
	}
	for _, t := range r.targets {
		if g := groups[t.Adapter.Name()]; len(g) > 0 {
			_ = t.Adapter.Run(ctx, t.Detection.RootDir, adapter.Selection{NodeIDs: g}, r.eng.Apply)
		}
	}
}

func scopeName(sel engine.Selection) string {
	switch {
	case sel.All:
		return "all"
	case sel.Failed:
		return "failed"
	default:
		return "selection"
	}
}
