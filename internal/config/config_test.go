package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPresetFor(t *testing.T) {
	cases := map[string]string{
		"code":    "code -g {file}:{line}",
		"cursor":  "cursor -g {file}:{line}",
		"nvim":    "nvim +{line} {file}",
		"vim":     "vim +{line} {file}",
		"goland":  "goland --line {line} {file}",
		"emacs":   "emacs +{line}:{col} {file}",
		"hx":      "hx {file}:{line}",
		"unknown": "unknown {file}",
		"":        "",
	}
	for in, want := range cases {
		if got := presetFor(in); got != want {
			t.Errorf("presetFor(%q) = %q, want %q", in, got, want)
		}
	}
	// A value with flags keeps them and selects the preset by binary base name.
	if got := presetFor("code -w"); got != "code -w -g {file}:{line}" {
		t.Errorf("presetFor(code -w) = %q", got)
	}
	// Absolute paths select by base name too.
	if got := presetFor("/usr/bin/nvim"); got != "/usr/bin/nvim +{line} {file}" {
		t.Errorf("presetFor(abs nvim) = %q", got)
	}
}

// isolateEnv clears all inputs Load consults so each test starts clean.
func isolateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty: no user config
	t.Setenv("TT_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFallsBackToEditorPreset(t *testing.T) {
	isolateEnv(t)
	t.Setenv("EDITOR", "nvim")
	if got := Load(t.TempDir()).Editor; got != "nvim +{line} {file}" {
		t.Fatalf("Editor = %q, want nvim preset", got)
	}
}

func TestLoadEmptyWhenNothingConfigured(t *testing.T) {
	isolateEnv(t)
	if got := Load(t.TempDir()).Editor; got != "" {
		t.Fatalf("Editor = %q, want empty", got)
	}
}

func TestLoadUserConfig(t *testing.T) {
	isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeFile(t, filepath.Join(xdg, "tt", "config.toml"), `editor = "vi {file}"`)

	if got := Load(t.TempDir()).Editor; got != "vi {file}" {
		t.Fatalf("Editor = %q, want user config value", got)
	}
}

func TestLoadProjectOverridesUserAndEnvOverridesAll(t *testing.T) {
	isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeFile(t, filepath.Join(xdg, "tt", "config.toml"), `editor = "user-editor {file}"`)

	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".tt.toml"), `editor = "project-editor {file}"`)

	// Project config wins over user config.
	if got := Load(proj).Editor; got != "project-editor {file}" {
		t.Fatalf("Editor = %q, want project override", got)
	}

	// $TT_EDITOR wins over everything.
	t.Setenv("TT_EDITOR", "env-editor {file}")
	if got := Load(proj).Editor; got != "env-editor {file}" {
		t.Fatalf("Editor = %q, want env override", got)
	}
}

func TestProjectConfigFoundInAncestor(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".tt.toml"), `editor = "root-editor {file}"`)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Load(sub).Editor; got != "root-editor {file}" {
		t.Fatalf("Editor = %q, want ancestor .tt.toml value", got)
	}
}
