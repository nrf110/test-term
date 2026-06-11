package gotest

import (
	"go/token"
	"path/filepath"
	"testing"
)

func TestTestFuncsParsesEntryPoints(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join("testdata", "projects", "sample", "math_test.go")
	got := testFuncs(fset, path)

	want := map[string]bool{
		"TestAdd": true, "TestAlwaysFails": true,
		"TestSkipped": true, "TestWithSubtests": true,
	}
	if len(got) != len(want) {
		t.Fatalf("found %d funcs (%v), want %d", len(got), names(got), len(want))
	}
	for _, fn := range got {
		if !want[fn.name] {
			t.Errorf("unexpected func %q", fn.name)
		}
		if fn.line <= 0 {
			t.Errorf("func %q has no line number", fn.name)
		}
	}
}

func TestIsTestEntryPoint(t *testing.T) {
	yes := []string{"Test", "TestFoo", "Example", "ExampleBar", "Fuzz", "FuzzX"}
	no := []string{"Testfoo", "Exampleish", "helper", "TestingT", "Benchmark", "BenchmarkX"}
	for _, n := range yes {
		if !isTestEntryPoint(n) {
			t.Errorf("isTestEntryPoint(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if isTestEntryPoint(n) {
			t.Errorf("isTestEntryPoint(%q) = true, want false", n)
		}
	}
}

func names(fns []testFunc) []string {
	out := make([]string, len(fns))
	for i, f := range fns {
		out[i] = f.name
	}
	return out
}
