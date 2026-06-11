package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootVersion(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "tt ") {
		t.Fatalf("version output = %q, want prefix %q", got, "tt ")
	}
	if strings.Contains(got, "{{") {
		t.Fatalf("version template not rendered: %q", got)
	}
}

func TestRootHelpFlag(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	if !strings.Contains(out.String(), "terminal-UI test runner") {
		t.Fatalf("expected help text, got %q", out.String())
	}
}

func TestIsExposed(t *testing.T) {
	loopback := []string{"127.0.0.1:7878", "localhost:7878", "[::1]:7878", "127.0.0.1", "::1"}
	exposed := []string{":7878", "0.0.0.0:7878", "[::]:7878", "0.0.0.0", "::", "", "192.168.1.5:7878", "example.com:7878"}

	for _, a := range loopback {
		if isExposed(a) {
			t.Errorf("isExposed(%q) = true, want false (loopback)", a)
		}
	}
	for _, a := range exposed {
		if !isExposed(a) {
			t.Errorf("isExposed(%q) = false, want true (must require token)", a)
		}
	}
}

func TestRootNoFrameworksDetected(t *testing.T) {
	// In an empty directory the root command reports no frameworks and exits 0,
	// without attempting to launch the TUI.
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{t.TempDir()})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute on empty dir: %v", err)
	}
	if !strings.Contains(out.String(), "No supported test frameworks") {
		t.Fatalf("expected no-frameworks message, got %q", out.String())
	}
}
