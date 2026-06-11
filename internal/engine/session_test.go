package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/nrf110/test-term/internal/event"
)

// apply is a tiny helper to drive a sequence of events into a session.
func apply(s *Session, evs ...event.Event) {
	for _, e := range evs {
		s.Apply(e)
	}
}

func find(roots []*Node, id string) *Node {
	for _, r := range roots {
		if n := findIn(r, id); n != nil {
			return n
		}
	}
	return nil
}

func findIn(n *Node, id string) *Node {
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if got := findIn(c, id); got != nil {
			return got
		}
	}
	return nil
}

func TestBuildsTreeWithParentChild(t *testing.T) {
	s := New()
	apply(s,
		event.NodeDiscovered("file", "", "auth.test.ts", event.KindFile, nil),
		event.NodeDiscovered("file::login", "file", "login", event.KindSuite, nil),
		event.NodeDiscovered("file::login::ok", "file::login", "accepts valid", event.KindTest, nil),
		event.NodeDiscovered("file::login::bad", "file::login", "rejects bad", event.KindTest, nil),
	)

	roots := s.Snapshot()
	if len(roots) != 1 || roots[0].ID != "file" {
		t.Fatalf("expected single root 'file', got %+v", roots)
	}
	login := find(roots, "file::login")
	if login == nil || len(login.Children) != 2 {
		t.Fatalf("expected suite with 2 tests, got %+v", login)
	}
	if login.Children[0].ID != "file::login::ok" || login.Children[1].ID != "file::login::bad" {
		t.Fatalf("children out of insertion order: %+v", login.Children)
	}
}

func TestStatusRollsUpToAncestors(t *testing.T) {
	s := New()
	apply(s,
		event.NodeDiscovered("f", "", "file", event.KindFile, nil),
		event.NodeDiscovered("f::a", "f", "a", event.KindTest, nil),
		event.NodeDiscovered("f::b", "f", "b", event.KindTest, nil),
	)

	// Both pending -> container pending.
	if got := find(s.Snapshot(), "f").Status; got != event.StatusPending {
		t.Fatalf("all-pending container status = %s, want pending", got)
	}

	// One running -> container running (active work dominates).
	apply(s, event.NodeStarted("f::a"))
	if got := find(s.Snapshot(), "f").Status; got != event.StatusRunning {
		t.Fatalf("container status with a running child = %s, want running", got)
	}

	// a passes, b fails -> container fails.
	apply(s,
		event.NodeFinished("f::a", event.StatusPass, 5, nil),
		event.NodeFinished("f::b", event.StatusFail, 3, &event.Failure{Message: "boom"}),
	)
	root := find(s.Snapshot(), "f")
	if root.Status != event.StatusFail {
		t.Fatalf("container status = %s, want fail", root.Status)
	}
	if root.DurationMs != 8 {
		t.Fatalf("container duration = %d, want 8 (sum of children)", root.DurationMs)
	}
}

func TestMidRunDiscovery(t *testing.T) {
	// Go subtests / pytest params appear during the run, under a parent that was
	// already discovered. The tree must absorb them without losing the parent.
	s := New()
	apply(s,
		event.NodeDiscovered("go::TestLogin", "", "TestLogin", event.KindTest, nil),
		event.NodeStarted("go::TestLogin"),
		// Subtest discovered mid-run:
		event.NodeDiscovered("go::TestLogin/sub", "go::TestLogin", "sub", event.KindTest, nil),
		event.NodeFinished("go::TestLogin/sub", event.StatusPass, 2, nil),
	)

	parent := find(s.Snapshot(), "go::TestLogin")
	if parent == nil || len(parent.Children) != 1 {
		t.Fatalf("expected parent with 1 subtest, got %+v", parent)
	}
	if parent.Status != event.StatusPass {
		t.Fatalf("parent should aggregate to pass once its only child passed, got %s", parent.Status)
	}
}

func TestSummaryComputedFromLeaves(t *testing.T) {
	s := New()
	apply(s,
		event.NodeDiscovered("f", "", "file", event.KindFile, nil),
		event.NodeDiscovered("f::p", "f", "p", event.KindTest, nil),
		event.NodeDiscovered("f::q", "f", "q", event.KindTest, nil),
		event.NodeDiscovered("f::r", "f", "r", event.KindTest, nil),
		event.NodeFinished("f::p", event.StatusPass, 5, nil),
		event.NodeFinished("f::q", event.StatusFail, 3, &event.Failure{Message: "x"}),
		event.NodeFinished("f::r", event.StatusSkip, 0, nil),
	)
	got := s.Summary()
	want := event.Summary{Total: 3, Passed: 1, Failed: 1, Skipped: 1, DurationMs: 8}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func TestSnapshotIsIsolated(t *testing.T) {
	s := New()
	apply(s, event.NodeDiscovered("n", "", "n", event.KindTest, nil))

	snap := s.Snapshot()
	snap[0].Name = "mutated"
	snap[0].Status = event.StatusFail

	live := s.Snapshot()
	if live[0].Name != "n" || live[0].Status == event.StatusFail {
		t.Fatalf("mutating a snapshot leaked into the live tree: %+v", live[0])
	}
}

func TestSubscribeSnapshotPlusLiveStream(t *testing.T) {
	s := New()
	apply(s, event.NodeDiscovered("a", "", "a", event.KindTest, nil))

	sub := s.Subscribe(16)
	defer sub.Close()

	if len(sub.Snapshot) != 1 || sub.Snapshot[0].ID != "a" {
		t.Fatalf("snapshot did not capture pre-subscribe state: %+v", sub.Snapshot)
	}

	apply(s, event.NodeStarted("a"), event.NodeFinished("a", event.StatusPass, 1, nil))

	got := []event.Type{(<-sub.C).Type, (<-sub.C).Type}
	want := []event.Type{event.TypeNodeStarted, event.TypeNodeFinished}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("live events = %v, want %v", got, want)
	}
}

func TestMultipleSubscribersAllReceive(t *testing.T) {
	s := New()
	sub1 := s.Subscribe(8)
	sub2 := s.Subscribe(8)
	defer sub1.Close()
	defer sub2.Close()

	apply(s, event.RunStarted("r1", "all"))

	if e := <-sub1.C; e.Type != event.TypeRunStarted {
		t.Fatalf("sub1 got %s", e.Type)
	}
	if e := <-sub2.C; e.Type != event.TypeRunStarted {
		t.Fatalf("sub2 got %s", e.Type)
	}
}

func TestSubscribeCloseStopsDelivery(t *testing.T) {
	s := New()
	sub := s.Subscribe(8)
	sub.Close()

	// After Close, Done is signalled and applying delivers nothing further
	// (and must not panic).
	select {
	case <-sub.Done():
	default:
		t.Fatal("Done() not signalled after Close()")
	}

	apply(s, event.RunStarted("r", "all"))
	select {
	case e := <-sub.C:
		t.Fatalf("received %s after Close(); expected no delivery", e.Type)
	default:
	}
}

func TestApplyDoesNotBlockOnClosedSubscriberDuringFanout(t *testing.T) {
	// Regression: a subscriber closed concurrently with a fan-out must never
	// cause a send on a closed channel, and a full buffer on a closed
	// subscriber must not wedge Apply.
	s := New()
	sub := s.Subscribe(1)
	sub.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.Apply(event.RunStarted("r", "all"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Apply wedged on a closed subscriber")
	}
}

func TestResolveAll(t *testing.T) {
	s := buildSampleTree()
	got := s.Resolve(Selection{All: true})
	want := []string{"f::a", "f::b", "g::c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(all) = %v, want %v", got, want)
	}
}

func TestResolveFailed(t *testing.T) {
	s := buildSampleTree()
	s.Apply(event.NodeFinished("f::a", event.StatusFail, 1, &event.Failure{Message: "x"}))
	s.Apply(event.NodeFinished("g::c", event.StatusError, 1, &event.Failure{Message: "y"}))
	got := s.Resolve(Selection{Failed: true})
	want := []string{"f::a", "g::c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(failed) = %v, want %v", got, want)
	}
}

func TestResolveContainerExpandsToLeavesInTreeOrder(t *testing.T) {
	s := buildSampleTree()
	// Selecting the file container 'f' and a single leaf 'g::c' out of order
	// should expand to f's leaves and return everything in tree order.
	got := s.Resolve(Selection{Nodes: []string{"g::c", "f"}})
	want := []string{"f::a", "f::b", "g::c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(nodes) = %v, want %v", got, want)
	}
}

func TestResolveIgnoresUnknownNodes(t *testing.T) {
	s := buildSampleTree()
	got := s.Resolve(Selection{Nodes: []string{"does-not-exist"}})
	if len(got) != 0 {
		t.Fatalf("Resolve(unknown) = %v, want empty", got)
	}
}

// buildSampleTree builds:
//
//	f (file)
//	  f::a (test)
//	  f::b (test)
//	g (file)
//	  g::c (test)
func buildSampleTree() *Session {
	s := New()
	apply(s,
		event.NodeDiscovered("f", "", "f", event.KindFile, nil),
		event.NodeDiscovered("f::a", "f", "a", event.KindTest, nil),
		event.NodeDiscovered("f::b", "f", "b", event.KindTest, nil),
		event.NodeDiscovered("g", "", "g", event.KindFile, nil),
		event.NodeDiscovered("g::c", "g", "c", event.KindTest, nil),
	)
	return s
}
