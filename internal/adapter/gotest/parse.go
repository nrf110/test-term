package gotest

import (
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// test2jsonEvent is one line of `go test -json` output (the test2json format).
// Package-level events omit Test; test-level events include it.
type test2jsonEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"` // seconds
	Output  string  `json:"Output"`
}

// fileLineRe matches a Go source reference like "    foo_test.go:42:" at the
// start of an output line (the form `go test` uses to report failures).
var fileLineRe = regexp.MustCompile(`(?m)^\s*([^\s:]+\.go):(\d+):`)

// runParser converts a stream of test2json events into normalized events. It is
// the heart of the adapter and is exercised directly by golden-file tests,
// independent of any subprocess.
type runParser struct {
	emit adapter.Emit
	// abs resolves a package-relative file (as printed by `go test`) to an
	// absolute path. It may be nil, in which case the file is left as-is.
	abs func(pkg, file string) string

	discovered map[string]bool             // node IDs already announced
	output     map[string]*strings.Builder // test node ID -> accumulated output
	tests      map[string]bool             // packages that ran at least one test
}

func newRunParser(emit adapter.Emit, abs func(pkg, file string) string) *runParser {
	return &runParser{
		emit:       emit,
		abs:        abs,
		discovered: map[string]bool{},
		output:     map[string]*strings.Builder{},
		tests:      map[string]bool{},
	}
}

// parse consumes the test2json stream to completion.
func (p *runParser) parse(r io.Reader) error {
	dec := json.NewDecoder(r)
	for {
		var te test2jsonEvent
		if err := dec.Decode(&te); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		p.handle(te)
	}
}

func (p *runParser) handle(te test2jsonEvent) {
	switch te.Action {
	case "run":
		p.ensurePkg(te.Package)
		p.ensureTest(te.Package, te.Test)
		p.tests[te.Package] = true
		p.emit(event.NodeStarted(testNodeID(te.Package, te.Test)))

	case "output":
		p.handleOutput(te)

	case "pass":
		if te.Test == "" {
			p.finishPkg(te.Package, event.StatusPass, te.Elapsed, nil)
			return
		}
		p.emit(event.NodeFinished(testNodeID(te.Package, te.Test), event.StatusPass, ms(te.Elapsed), nil))

	case "skip":
		if te.Test == "" {
			// Package with no test files; do not clutter the tree with it.
			return
		}
		p.emit(event.NodeFinished(testNodeID(te.Package, te.Test), event.StatusSkip, ms(te.Elapsed), nil))

	case "fail":
		if te.Test == "" {
			p.finishFailedPkg(te.Package, te.Elapsed)
			return
		}
		id := testNodeID(te.Package, te.Test)
		fail := p.failureFor(te.Package, id)
		p.emit(event.NodeFinished(id, event.StatusFail, ms(te.Elapsed), fail))

	case "start", "pause", "cont", "bench":
		// start: package began — defer node creation until a test or failure.
		// pause/cont: parallel scheduling noise. bench: not run in v1.
	}
}

func (p *runParser) handleOutput(te test2jsonEvent) {
	if te.Test == "" {
		// Package-level output: keep for build-failure reporting.
		p.appendOutput(pkgNodeID(te.Package), te.Output)
		p.emit(event.Output(pkgNodeID(te.Package), event.StreamStdout, te.Output))
		return
	}
	id := testNodeID(te.Package, te.Test)
	// Skip the administrative framing lines; keep real output for the failure
	// message and stream the rest to observers.
	trimmed := strings.TrimLeft(te.Output, " \t")
	if strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") {
		return
	}
	p.appendOutput(id, te.Output)
	p.emit(event.Output(id, event.StreamStdout, te.Output))
}

func (p *runParser) appendOutput(id, s string) {
	b := p.output[id]
	if b == nil {
		b = &strings.Builder{}
		p.output[id] = b
	}
	b.WriteString(s)
}

// ensurePkg announces a package node once.
func (p *runParser) ensurePkg(importPath string) {
	id := pkgNodeID(importPath)
	if p.discovered[id] {
		return
	}
	p.discovered[id] = true
	p.emit(event.NodeDiscovered(id, "", importPath, event.KindFile, nil))
}

// ensureTest announces a test node and any missing ancestor subtests, parents
// first, each exactly once.
func (p *runParser) ensureTest(importPath, test string) {
	id := testNodeID(importPath, test)
	if p.discovered[id] {
		return
	}
	if i := strings.LastIndex(test, "/"); i >= 0 {
		p.ensureTest(importPath, test[:i]) // ensure parent subtest first
	} else {
		p.ensurePkg(importPath)
	}
	p.discovered[id] = true
	p.emit(event.NodeDiscovered(id, parentNodeID(importPath, test), leafName(test), event.KindTest, nil))
}

func (p *runParser) finishPkg(importPath string, status event.Status, elapsed float64, fail *event.Failure) {
	id := pkgNodeID(importPath)
	if !p.discovered[id] {
		return // never had tests; skip
	}
	p.emit(event.NodeFinished(id, status, ms(elapsed), fail))
}

// finishFailedPkg handles a package that fails. If it ran tests, the failure is
// already attributed to those tests and the container aggregates to fail. If it
// ran none, the failure is a build/setup error: surface it on the package node.
func (p *runParser) finishFailedPkg(importPath string, elapsed float64) {
	if p.tests[importPath] {
		p.emit(event.NodeFinished(pkgNodeID(importPath), event.StatusFail, ms(elapsed), nil))
		return
	}
	p.ensurePkg(importPath)
	msg := strings.TrimSpace(p.builtOutput(pkgNodeID(importPath)))
	if msg == "" {
		msg = "package failed to build or run"
	}
	p.emit(event.NodeFinished(pkgNodeID(importPath), event.StatusError, ms(elapsed),
		&event.Failure{Message: msg}))
}

// failureFor builds a Failure from a failed test's accumulated output, parsing
// the first file:line reference as the primary frame.
func (p *runParser) failureFor(importPath, nodeID string) *event.Failure {
	out := p.builtOutput(nodeID)
	if strings.TrimSpace(out) == "" {
		return &event.Failure{Message: "test failed"}
	}

	f := &event.Failure{Message: strings.TrimRight(out, "\n")}
	for _, m := range fileLineRe.FindAllStringSubmatch(out, -1) {
		file := m[1]
		line, _ := strconv.Atoi(m[2]) // regex guarantees digits
		if p.abs != nil {
			file = p.abs(importPath, file)
		}
		f.Frames = append(f.Frames, event.Frame{File: file, Line: line})
	}
	return f
}

func (p *runParser) builtOutput(nodeID string) string {
	if b := p.output[nodeID]; b != nil {
		return b.String()
	}
	return ""
}

// ms converts test2json seconds (a float) to integer milliseconds.
func ms(seconds float64) int64 { return int64(seconds*1000 + 0.5) }
