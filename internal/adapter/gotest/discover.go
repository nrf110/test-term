package gotest

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nrf110/test-term/internal/adapter"
	"github.com/nrf110/test-term/internal/event"
)

// pkgInfo is the subset of `go list -json` output we consume.
type pkgInfo struct {
	ImportPath   string
	Dir          string
	TestGoFiles  []string // *_test.go in the package
	XTestGoFiles []string // *_test.go in the external <pkg>_test package
}

// goList runs `go list -json ./...` in dir and returns one pkgInfo per package.
// `go list` is authoritative about which packages and test files exist and
// honors build constraints.
func goList(ctx context.Context, dir string) ([]pkgInfo, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		// `go list` can emit partial results alongside errors (e.g. one broken
		// package); proceed with whatever parsed if we got any.
		pkgs, perr := decodePkgList(&out)
		if perr == nil && len(pkgs) > 0 {
			return pkgs, nil
		}
		return nil, &CommandError{Cmd: "go list", Stderr: errb.String(), Err: err}
	}
	return decodePkgList(&out)
}

func decodePkgList(r io.Reader) ([]pkgInfo, error) {
	dec := json.NewDecoder(r)
	var pkgs []pkgInfo
	for {
		var p pkgInfo
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				return pkgs, nil
			}
			return pkgs, err
		}
		pkgs = append(pkgs, p)
	}
}

// CommandError reports a failed external command with its stderr.
type CommandError struct {
	Cmd    string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	msg := e.Cmd + ": " + e.Err.Error()
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "\n" + s
	}
	return msg
}

func (e *CommandError) Unwrap() error { return e.Err }

// discoverPackages emits NodeDiscovered events for every package and test
// function found via `go list` + AST parsing, and returns the maps the adapter
// caches: import path -> dir, and node ID -> source location.
func discoverPackages(pkgs []pkgInfo, emit adapter.Emit) (dirs map[string]string, locs map[string]event.Location) {
	dirs = make(map[string]string)
	locs = make(map[string]event.Location)
	fset := token.NewFileSet()

	for _, p := range pkgs {
		dirs[p.ImportPath] = p.Dir
		files := append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...)
		if len(files) == 0 {
			continue
		}

		pkgEmitted := false
		for _, name := range files {
			path := filepath.Join(p.Dir, name)
			for _, fn := range testFuncs(fset, path) {
				if !pkgEmitted {
					emit(event.NodeDiscovered(pkgNodeID(p.ImportPath), "", p.ImportPath, event.KindFile, nil))
					pkgEmitted = true
				}
				loc := event.Location{File: path, Line: fn.line}
				id := testNodeID(p.ImportPath, fn.name)
				locs[id] = loc
				l := loc
				emit(event.NodeDiscovered(id, pkgNodeID(p.ImportPath), fn.name, event.KindTest, &l))
			}
		}
	}
	return dirs, locs
}

type testFunc struct {
	name string
	line int
}

// testFuncs parses a Go file and returns its test entry points: functions named
// Test*, Example*, or Fuzz* with a single *testing.{T,F,M}-style parameter.
// Benchmarks are intentionally excluded (not run in v1).
func testFuncs(fset *token.FileSet, path string) []testFunc {
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil // unparseable file: skip; `go test` will surface real errors
	}
	var out []testFunc
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil {
			continue
		}
		name := fd.Name.Name
		if !isTestEntryPoint(name) {
			continue
		}
		// Require exactly one parameter (e.g. *testing.T) to avoid matching
		// helpers like "TestHelper(t, x)". Examples may take none.
		params := 0
		if fd.Type.Params != nil {
			for _, fld := range fd.Type.Params.List {
				n := len(fld.Names)
				if n == 0 {
					n = 1
				}
				params += n
			}
		}
		if strings.HasPrefix(name, "Example") {
			if params != 0 {
				continue
			}
		} else if params != 1 {
			continue
		}
		out = append(out, testFunc{name: name, line: fset.Position(fd.Pos()).Line})
	}
	return out
}

func isTestEntryPoint(name string) bool {
	for _, pre := range []string{"Test", "Example", "Fuzz"} {
		if strings.HasPrefix(name, pre) && len(name) > len(pre) {
			// Next rune must not be lowercase (Go's own rule: TestFoo, not Testfoo).
			c := name[len(pre)]
			if c >= 'a' && c <= 'z' {
				return false
			}
			return true
		}
		if name == pre {
			return true // bare "Test"/"Example"/"Fuzz" are valid entry points
		}
	}
	return false
}
