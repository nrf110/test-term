// Package adapter defines the contract every test-framework integration
// implements, plus a registry for detecting which adapters apply to a project.
//
// An adapter has four jobs: report whether it applies to a directory (Detect),
// list a project's tests without running them (Discover), execute tests and
// stream results (Run), and map a node back to a source location (Locate).
// Everything an adapter emits is in the normalized event vocabulary, so no other
// layer needs framework-specific knowledge.
package adapter

import (
	"context"

	"github.com/nrf110/test-term/internal/event"
)

// Emit delivers a normalized event to the engine.
type Emit func(event.Event)

// Detection is what an adapter reports when it recognizes a project.
type Detection struct {
	// RootDir is the directory the adapter considers the project root (e.g. a Go
	// module root). Discover and Run are invoked relative to it.
	RootDir string
}

// Selection restricts a run. Empty NodeIDs means "run everything".
type Selection struct {
	// NodeIDs limits the run to these nodes; containers expand to their
	// descendants. The engine resolves higher-level intents (all/failed) into
	// concrete node IDs before calling Run.
	NodeIDs []string
}

// Adapter integrates a single test framework.
type Adapter interface {
	// Name is a short, stable identifier (e.g. "go", "vitest", "pytest").
	Name() string

	// Detect reports whether this adapter applies to dir, and if so where it is
	// rooted. It must be cheap and side-effect free.
	Detect(dir string) (Detection, bool)

	// Discover lists tests without executing them, emitting NodeDiscovered
	// events to build the tree. It should populate node locations where cheaply
	// available.
	Discover(ctx context.Context, dir string, emit Emit) error

	// Run executes the selected tests (all if the selection is empty) and streams
	// normalized events. It owns its subprocess and output parsing.
	Run(ctx context.Context, dir string, sel Selection, emit Emit) error

	// Locate returns the source location for a node, if known.
	Locate(nodeID string) (event.Location, bool)
}

// Detected pairs an adapter with where it was detected.
type Detected struct {
	Adapter   Adapter
	Detection Detection
}

// Registry holds the available adapters and detects which apply to a project.
type Registry struct {
	adapters []Adapter
}

// NewRegistry returns a registry seeded with the given adapters.
func NewRegistry(adapters ...Adapter) *Registry {
	return &Registry{adapters: append([]Adapter(nil), adapters...)}
}

// Register adds an adapter to the registry.
func (r *Registry) Register(a Adapter) { r.adapters = append(r.adapters, a) }

// Adapters returns the registered adapters in registration order.
func (r *Registry) Adapters() []Adapter { return append([]Adapter(nil), r.adapters...) }

// Detect returns every adapter that applies to dir, in registration order. A
// polyglot project (e.g. Go + Vitest + pytest in a monorepo) yields multiple.
func (r *Registry) Detect(dir string) []Detected {
	var out []Detected
	for _, a := range r.adapters {
		if d, ok := a.Detect(dir); ok {
			out = append(out, Detected{Adapter: a, Detection: d})
		}
	}
	return out
}

// Find returns the registered adapter with the given name, or nil.
func (r *Registry) Find(name string) Adapter {
	for _, a := range r.adapters {
		if a.Name() == name {
			return a
		}
	}
	return nil
}
