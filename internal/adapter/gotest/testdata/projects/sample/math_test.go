package sample

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatalf("Add(1,2) = %d, want 3", Add(1, 2))
	}
}

func TestAlwaysFails(t *testing.T) {
	t.Errorf("boom: expected 1, got 2")
}

func TestSkipped(t *testing.T) {
	t.Skip("not relevant here")
}

func TestWithSubtests(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("bad", func(t *testing.T) {
		t.Fatal("subtest failed on purpose")
	})
}
