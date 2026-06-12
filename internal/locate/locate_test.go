package locate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func argv(t *testing.T, template string, tg Target) []string {
	t.Helper()
	cmd, err := Command(template, tg)
	if err != nil {
		t.Fatalf("Command(%q): %v", template, err)
	}
	return cmd.Args
}

func TestCommandExpandsPlaceholders(t *testing.T) {
	cases := []struct {
		template string
		want     []string
	}{
		{"code -g {file}:{line}", []string{"code", "-g", "/a/b.go:42"}},
		{"nvim +{line} {file}", []string{"nvim", "+42", "/a/b.go"}},
		{"goland --line {line} {file}", []string{"goland", "--line", "42", "/a/b.go"}},
		{"emacs +{line}:{col} {file}", []string{"emacs", "+42:7", "/a/b.go"}},
	}
	for _, c := range cases {
		got := argv(t, c.template, Target{File: "/a/b.go", Line: 42, Col: 7})
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("template %q -> %v, want %v", c.template, got, c.want)
		}
	}
}

func TestCommandKeepsPathWithSpacesAsOneArg(t *testing.T) {
	// The whole point of token-first substitution: a spaced path is one argv
	// element, immune to word-splitting and shell injection.
	got := argv(t, "code -g {file}:{line}", Target{File: "/my projects/a b.go", Line: 5})
	if len(got) != 3 {
		t.Fatalf("argv = %v, want 3 elements", got)
	}
	if got[2] != "/my projects/a b.go:5" {
		t.Errorf("file arg = %q, want the spaced path joined with line", got[2])
	}
}

func TestCommandClampsLineAndCol(t *testing.T) {
	got := argv(t, "nvim +{line} {file}", Target{File: "/a.go", Line: 0})
	if got[1] != "+1" {
		t.Errorf("line arg = %q, want +1 (clamped from 0)", got[1])
	}
}

func TestCommandRunsRealEditorPreservingSpacedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as a fake editor")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	editor := filepath.Join(dir, "fake-editor.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsFile) + "\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	spaced := filepath.Join(dir, "a b.go")
	cmd, err := Command(editor+" -g {file}:{line}", Target{File: spaced, Line: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("run fake editor: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	// The editor must have received exactly two args: "-g" and the spaced
	// path joined with the line — the spaced path NOT split apart.
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	want := []string{"-g", spaced + ":7"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("editor received %v, want %v", lines, want)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func TestCommandNoEditor(t *testing.T) {
	if _, err := Command("", Target{File: "/a.go"}); err != ErrNoEditor {
		t.Errorf("Command(\"\") err = %v, want ErrNoEditor", err)
	}
	if _, err := Command("   ", Target{File: "/a.go"}); err != ErrNoEditor {
		t.Errorf("Command(whitespace) err = %v, want ErrNoEditor", err)
	}
}
