package gotest

import "testing"

func TestNodeIDRoundTrip(t *testing.T) {
	cases := []struct {
		pkg, test string
	}{
		{"example.com/m", "TestA"},
		{"example.com/m", "TestA/sub"},
		{"example.com/m", "TestA/sub/deep"},
		{"example.com/m", ""}, // package node
	}
	for _, c := range cases {
		var id string
		if c.test == "" {
			id = pkgNodeID(c.pkg)
		} else {
			id = testNodeID(c.pkg, c.test)
		}
		pkg, test, ok := splitNodeID(id)
		if !ok || pkg != c.pkg || test != c.test {
			t.Fatalf("splitNodeID(%q) = (%q,%q,%v), want (%q,%q,true)", id, pkg, test, ok, c.pkg, c.test)
		}
	}
}

func TestParentNodeID(t *testing.T) {
	if got := parentNodeID("p", "TestA"); got != "go:p" {
		t.Errorf("top-level parent = %q, want package node go:p", got)
	}
	if got := parentNodeID("p", "TestA/sub"); got != "go:p::TestA" {
		t.Errorf("subtest parent = %q, want go:p::TestA", got)
	}
	if got := parentNodeID("p", "TestA/sub/deep"); got != "go:p::TestA/sub" {
		t.Errorf("nested parent = %q, want go:p::TestA/sub", got)
	}
}

func TestLeafNameAndTopLevel(t *testing.T) {
	if got := leafName("TestA/sub_b"); got != "sub_b" {
		t.Errorf("leafName = %q, want sub_b", got)
	}
	if got := leafName("TestA"); got != "TestA" {
		t.Errorf("leafName = %q, want TestA", got)
	}
	if got := topLevelTest("TestA/sub/deep"); got != "TestA" {
		t.Errorf("topLevelTest = %q, want TestA", got)
	}
}

func TestSplitNonGoNode(t *testing.T) {
	if _, _, ok := splitNodeID("vitest:foo::bar"); ok {
		t.Errorf("splitNodeID should reject non-go IDs")
	}
}
