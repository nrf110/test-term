package vitest

import "strings"

// Node ID scheme, keyed off the test file path relative to the project root so
// glob-based discovery and run-reporter parsing agree on file nodes:
//
//	file:   vitest:<relpath>
//	suite:  vitest:<relpath>::<describe>[::<describe>...]
//	test:   vitest:<relpath>::<describe>...::<title>
const (
	prefix = "vitest:"
	sep    = "::"
)

func fileNodeID(relPath string) string { return prefix + relPath }

func childID(parentID, name string) string { return parentID + sep + name }

// splitNodeID returns the relative file path of a vitest node, and whether the
// ID belongs to this adapter.
func splitNodeID(id string) (relPath string, ok bool) {
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	rest := id[len(prefix):]
	if i := strings.Index(rest, sep); i >= 0 {
		return rest[:i], true
	}
	return rest, true
}
