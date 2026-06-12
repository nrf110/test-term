package pytest

import "strings"

// Node IDs are the pytest nodeid prefixed with "pytest:". Both discovery
// (--collect-only) and run (--report-log) speak pytest nodeids, so the IDs match
// exactly. The nodeid structure file::Class[::Class...]::test maps directly onto
// file -> suite(s) -> test nodes.
const prefix = "pytest:"

func nodeID(pytestNodeID string) string { return prefix + pytestNodeID }

// toPytestNodeID strips the prefix, returning the bare pytest nodeid and whether
// the ID belongs to this adapter.
func toPytestNodeID(id string) (string, bool) {
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	return id[len(prefix):], true
}
