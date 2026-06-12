// Package locate turns an editor command template plus a source location into a
// runnable command. Substitution is token-aware (the template is split into
// arguments first, then placeholders are filled within each token), so a file
// path with spaces stays a single argument and there is no shell involved —
// hence no command-injection surface.
package locate

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// Target is the location to open.
type Target struct {
	File string
	Line int
	Col  int
}

// ErrNoEditor indicates no editor command template is configured.
var ErrNoEditor = errors.New("no editor configured")

// Command builds the command to open t using the given template. Recognized
// placeholders: {file}, {line}, {col}. Line/Col below 1 are clamped to 1 so
// editors that reject ":0" still open the file.
func Command(template string, t Target) (*exec.Cmd, error) {
	if strings.TrimSpace(template) == "" {
		return nil, ErrNoEditor
	}
	tokens := strings.Fields(template)
	if len(tokens) == 0 {
		return nil, ErrNoEditor
	}

	line, col := t.Line, t.Col
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	repl := strings.NewReplacer(
		"{file}", t.File,
		"{line}", strconv.Itoa(line),
		"{col}", strconv.Itoa(col),
	)

	argv := make([]string, len(tokens))
	for i, tok := range tokens {
		argv[i] = repl.Replace(tok)
	}
	return exec.Command(argv[0], argv[1:]...), nil
}
