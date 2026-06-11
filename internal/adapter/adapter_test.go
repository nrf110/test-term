package adapter

import (
	"context"
	"testing"

	"github.com/nrf110/test-term/internal/event"
)

// fakeAdapter is a minimal adapter for registry tests.
type fakeAdapter struct {
	name     string
	detectAt string // dir at which it "applies"
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Detect(dir string) (Detection, bool) {
	if dir == f.detectAt {
		return Detection{RootDir: dir}, true
	}
	return Detection{}, false
}
func (f *fakeAdapter) Discover(context.Context, string, Emit) error       { return nil }
func (f *fakeAdapter) Run(context.Context, string, Selection, Emit) error { return nil }
func (f *fakeAdapter) Locate(string) (event.Location, bool)               { return event.Location{}, false }

func TestRegistryDetectReturnsApplicableInOrder(t *testing.T) {
	a := &fakeAdapter{name: "a", detectAt: "/proj"}
	b := &fakeAdapter{name: "b", detectAt: "/other"}
	c := &fakeAdapter{name: "c", detectAt: "/proj"}
	r := NewRegistry(a, b, c)

	got := r.Detect("/proj")
	if len(got) != 2 || got[0].Adapter.Name() != "a" || got[1].Adapter.Name() != "c" {
		t.Fatalf("Detect(/proj) = %v, want [a c] in registration order", names(got))
	}
	if got[0].Detection.RootDir != "/proj" {
		t.Fatalf("detection root = %q, want /proj", got[0].Detection.RootDir)
	}
}

func TestRegistryFind(t *testing.T) {
	r := NewRegistry(&fakeAdapter{name: "go"})
	r.Register(&fakeAdapter{name: "vitest"})
	if r.Find("vitest") == nil {
		t.Error("Find(vitest) = nil, want adapter")
	}
	if r.Find("missing") != nil {
		t.Error("Find(missing) should be nil")
	}
}

func names(d []Detected) []string {
	out := make([]string, len(d))
	for i, x := range d {
		out[i] = x.Adapter.Name()
	}
	return out
}
