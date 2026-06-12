package pytest

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// builder turns pytest nodeids into the file -> suite(s) -> test node hierarchy,
// emitting each NodeDiscovered once. When a location becomes available later (at
// run time), it re-emits the test node so the engine can fill it in.
type builder struct {
	emit       adapter.Emit
	discovered map[string]bool
	located    map[string]bool
}

func newBuilder(emit adapter.Emit) *builder {
	return &builder{emit: emit, discovered: map[string]bool{}, located: map[string]bool{}}
}

// ensure creates the chain of nodes for a pytest nodeid and returns the test
// node's ID. loc, if non-nil, is attached to the leaf test node.
func (b *builder) ensure(pytestNodeID string, loc *event.Location) string {
	parts := strings.Split(pytestNodeID, "::")
	parentID := ""
	for i := range parts {
		id := nodeID(strings.Join(parts[:i+1], "::"))
		kind := event.KindSuite
		switch {
		case i == 0:
			kind = event.KindFile
		case i == len(parts)-1:
			kind = event.KindTest
		}
		var l *event.Location
		if i == len(parts)-1 {
			l = loc
		}
		b.node(id, parentID, parts[i], kind, l)
		parentID = id
	}
	return nodeID(pytestNodeID)
}

func (b *builder) node(id, parent, name string, kind event.Kind, loc *event.Location) {
	if b.discovered[id] {
		// Fill in a location discovered later (e.g. at run time) exactly once.
		if loc != nil && !b.located[id] {
			b.located[id] = true
			b.emit(event.NodeDiscovered(id, parent, name, kind, loc))
		}
		return
	}
	b.discovered[id] = true
	if loc != nil {
		b.located[id] = true
	}
	b.emit(event.NodeDiscovered(id, parent, name, kind, loc))
}

// parseCollect reads `pytest --collect-only -q` output: one nodeid per line until
// a blank line / summary. Lines containing "::" are test nodeids.
func parseCollect(r io.Reader, emit adapter.Emit) error {
	b := newBuilder(emit)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			break
		}
		if strings.Contains(line, "::") {
			b.ensure(line, nil)
		}
	}
	return sc.Err()
}

// --- report-log (run) parsing --------------------------------------------

type report struct {
	ReportType string          `json:"$report_type"`
	NodeID     string          `json:"nodeid"`
	When       string          `json:"when"`
	Outcome    string          `json:"outcome"`
	Duration   float64         `json:"duration"`
	Location   []any           `json:"location"` // [relpath, 0-based line, domain]
	Longrepr   json.RawMessage `json:"longrepr"`
}

type longreprDict struct {
	Reprcrash struct {
		Path    string `json:"path"`
		Lineno  int    `json:"lineno"`
		Message string `json:"message"`
	} `json:"reprcrash"`
}

// runParser consumes a pytest-reportlog JSONL stream into normalized events.
type runParser struct {
	b       *builder
	rootDir string
	emit    adapter.Emit
	dur     map[string]float64        // nodeid -> accumulated seconds (setup+call)
	locs    map[string]event.Location // nodeID -> location, for Locate caching
}

func newRunParser(emit adapter.Emit, rootDir string) *runParser {
	return &runParser{
		b:       newBuilder(emit),
		rootDir: rootDir,
		emit:    emit,
		dur:     map[string]float64{},
		locs:    map[string]event.Location{},
	}
}

func (p *runParser) parse(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // failures can be large
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rep report
		if err := json.Unmarshal([]byte(line), &rep); err != nil {
			continue // tolerate non-JSON or unexpected lines
		}
		if rep.ReportType == "TestReport" {
			p.handle(rep)
		}
	}
	return sc.Err()
}

func (p *runParser) handle(rep report) {
	loc := p.location(rep)
	id := p.b.ensure(rep.NodeID, loc)
	p.dur[rep.NodeID] += rep.Duration

	switch rep.When {
	case "setup":
		switch rep.Outcome {
		case "skipped":
			p.emit(event.NodeFinished(id, event.StatusSkip, p.ms(rep.NodeID), nil))
		case "failed":
			// An error during setup: the test never ran its body.
			p.emit(event.NodeFinished(id, event.StatusError, p.ms(rep.NodeID), p.failure(rep)))
		default:
			p.emit(event.NodeStarted(id))
		}
	case "call":
		switch rep.Outcome {
		case "passed":
			p.emit(event.NodeFinished(id, event.StatusPass, p.ms(rep.NodeID), nil))
		case "failed":
			p.emit(event.NodeFinished(id, event.StatusFail, p.ms(rep.NodeID), p.failure(rep)))
		case "skipped":
			p.emit(event.NodeFinished(id, event.StatusSkip, p.ms(rep.NodeID), nil))
		}
	}
}

// location builds the test's source location from the report's location tuple,
// resolving the relative path against the project root. pytest's lineno is
// 0-based, so add one.
func (p *runParser) location(rep report) *event.Location {
	if len(rep.Location) < 2 {
		return nil
	}
	rel, ok := rep.Location[0].(string)
	if !ok || rel == "" {
		return nil
	}
	line := 0
	if f, ok := rep.Location[1].(float64); ok {
		line = int(f) + 1
	}
	loc := event.Location{File: filepath.Join(p.rootDir, rel), Line: line}
	p.locs[nodeID(rep.NodeID)] = loc
	return &loc
}

// failure extracts a Failure from a report's longrepr dict (reprcrash).
func (p *runParser) failure(rep report) *event.Failure {
	if len(rep.Longrepr) == 0 {
		return &event.Failure{Message: "test failed"}
	}
	var lr longreprDict
	if err := json.Unmarshal(rep.Longrepr, &lr); err != nil || lr.Reprcrash.Path == "" {
		return &event.Failure{Message: "test failed"}
	}
	f := &event.Failure{Message: lr.Reprcrash.Message}
	f.Frames = []event.Frame{{File: lr.Reprcrash.Path, Line: lr.Reprcrash.Lineno}}
	return f
}

func (p *runParser) ms(nodeID string) int64 { return int64(p.dur[nodeID]*1000 + 0.5) }
