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

func TestRootNoArgsShowsHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute with no args: %v", err)
	}

	if !strings.Contains(out.String(), "terminal-UI test runner") {
		t.Fatalf("expected help text, got %q", out.String())
	}
}
