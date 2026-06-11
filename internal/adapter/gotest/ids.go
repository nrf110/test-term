package gotest

import "strings"

// Node ID scheme. Both discovery and execution must produce identical IDs, so
// they key off the Go import path (stable across both `go list` and
// `go test -json`):
//
//	package:  go:<importpath>
//	test:     go:<importpath>::<TestName>
//	subtest:  go:<importpath>::<TestName>/<sub>   (Test name carries the slash)
const (
	prefix = "go:"
	sep    = "::"
)

func pkgNodeID(importPath string) string { return prefix + importPath }

func testNodeID(importPath, test string) string {
	return prefix + importPath + sep + test
}

// parentNodeID returns the ID of a test's parent: its enclosing subtest if it is
// nested, otherwise the package node.
func parentNodeID(importPath, test string) string {
	if i := strings.LastIndex(test, "/"); i >= 0 {
		return testNodeID(importPath, test[:i])
	}
	return pkgNodeID(importPath)
}

// leafName is the display name for a test node: the final path segment for a
// subtest, or the whole name for a top-level test.
func leafName(test string) string {
	if i := strings.LastIndex(test, "/"); i >= 0 {
		return test[i+1:]
	}
	return test
}

// splitNodeID decomposes a node ID into its import path and test name. test is
// empty for a package node. ok is false if the ID is not a gotest node.
func splitNodeID(id string) (importPath, test string, ok bool) {
	if !strings.HasPrefix(id, prefix) {
		return "", "", false
	}
	rest := id[len(prefix):]
	if i := strings.Index(rest, sep); i >= 0 {
		return rest[:i], rest[i+len(sep):], true
	}
	return rest, "", true
}

// topLevelTest returns the outermost test name (before the first "/"), which is
// what `go test -run` matches against.
func topLevelTest(test string) string {
	if i := strings.Index(test, "/"); i >= 0 {
		return test[:i]
	}
	return test
}
