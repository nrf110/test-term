package vitest

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// jsonReport is the top level of `vitest run --reporter=json` output (a
// Jest-compatible shape).
type jsonReport struct {
	TestResults []fileResult `json:"testResults"`
}

type fileResult struct {
	Name             string      `json:"name"` // absolute file path
	AssertionResults []assertion `json:"assertionResults"`
}

type assertion struct {
	AncestorTitles  []string `json:"ancestorTitles"`
	Title           string   `json:"title"`
	Status          string   `json:"status"` // passed|failed|skipped|todo
	Duration        float64  `json:"duration"`
	FailureMessages []string `json:"failureMessages"`
}

// frameRe matches a JS/TS source reference (path:line:col) inside a stack trace.
var frameRe = regexp.MustCompile(`([^\s():]+\.(?:js|jsx|ts|tsx|mjs|cjs)):(\d+):(\d+)`)

// runParser turns a vitest JSON report into normalized events. The report is
// terminal (emitted once at the end), so each test yields a NodeDiscovered
// followed immediately by a NodeFinished.
type runParser struct {
	emit       adapter.Emit
	rel        func(absFile string) string // absolute path -> project-relative
	discovered map[string]bool
	locs       map[string]event.Location // node ID -> location (for Locate caching)
}

func newRunParser(emit adapter.Emit, rel func(string) string) *runParser {
	return &runParser{
		emit:       emit,
		rel:        rel,
		discovered: map[string]bool{},
		locs:       map[string]event.Location{},
	}
}

// parse consumes the whole report. Vitest writes pure JSON to stdout, but be
// tolerant of leading/trailing noise by extracting the outer object.
func (p *runParser) parse(data []byte) error {
	var rep jsonReport
	if err := json.Unmarshal(extractJSON(data), &rep); err != nil {
		return err
	}
	for _, fr := range rep.TestResults {
		p.handleFile(fr)
	}
	return nil
}

func (p *runParser) handleFile(fr fileResult) {
	relPath := p.rel(fr.Name)
	fileID := fileNodeID(relPath)
	p.ensure(fileID, "", relPath, event.KindFile, nil)

	for _, a := range fr.AssertionResults {
		parent := fileID
		for _, anc := range a.AncestorTitles {
			cid := childID(parent, anc)
			p.ensure(cid, parent, anc, event.KindSuite, nil)
			parent = cid
		}
		testID := childID(parent, a.Title)

		fail := failureFrom(a.FailureMessages)
		var loc *event.Location
		if fail != nil && len(fail.Frames) > 0 {
			loc = &event.Location{File: fail.Frames[0].File, Line: fail.Frames[0].Line, Col: fail.Frames[0].Col}
		}
		p.ensure(testID, parent, a.Title, event.KindTest, loc)
		p.emit(event.NodeFinished(testID, status(a.Status), int64(a.Duration+0.5), fail))
	}
}

// ensure emits a NodeDiscovered for id exactly once, caching any location.
func (p *runParser) ensure(id, parentID, name string, kind event.Kind, loc *event.Location) {
	if p.discovered[id] {
		return
	}
	p.discovered[id] = true
	if loc != nil {
		p.locs[id] = *loc
	}
	p.emit(event.NodeDiscovered(id, parentID, name, kind, loc))
}

func status(s string) event.Status {
	switch s {
	case "passed":
		return event.StatusPass
	case "failed":
		return event.StatusFail
	case "skipped", "todo":
		return event.StatusSkip
	default:
		return event.StatusSkip
	}
}

// failureFrom builds a Failure from vitest failureMessages, parsing the first
// stack frame that points at user code (not node_modules).
func failureFrom(messages []string) *event.Failure {
	if len(messages) == 0 {
		return nil
	}
	msg := strings.Join(messages, "\n\n")
	f := &event.Failure{Message: strings.TrimSpace(msg)}
	for _, m := range frameRe.FindAllStringSubmatch(msg, -1) {
		if strings.Contains(m[1], "node_modules") {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		f.Frames = append(f.Frames, event.Frame{File: m[1], Line: line, Col: col})
		break
	}
	return f
}

// extractJSON returns the outermost {...} object from data, or data unchanged if
// no braces are found.
func extractJSON(data []byte) []byte {
	start := bytes.IndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start >= 0 && end > start {
		return data[start : end+1]
	}
	return data
}
