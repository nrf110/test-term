// Package config loads tt's user and project configuration. The only setting so
// far is the editor command template used to jump to a failing test.
//
// Resolution order (later wins): user config (~/.config/tt/config.toml) <
// project config (nearest .tt.toml above the working dir) < $TT_EDITOR <
// (fallback) a preset derived from $VISUAL/$EDITOR.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds resolved settings.
type Config struct {
	// Editor is a command template with {file}, {line}, {col} placeholders,
	// e.g. "code -g {file}:{line}". Empty means no editor is configured.
	Editor string `toml:"editor"`
}

// Load resolves configuration, searching for a project config starting at
// startDir. It never returns an error: unreadable or malformed files are
// ignored so a bad config can't stop the app from running.
func Load(startDir string) Config {
	var cfg Config

	if path := userConfigPath(); path != "" {
		merge(&cfg, path)
	}
	if path := projectConfigPath(startDir); path != "" {
		merge(&cfg, path)
	}
	if env := strings.TrimSpace(os.Getenv("TT_EDITOR")); env != "" {
		cfg.Editor = env
	}
	if cfg.Editor == "" {
		cfg.Editor = presetFor(firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR")))
	}
	return cfg
}

// merge reads a config file and overlays its non-empty fields onto cfg.
func merge(cfg *Config, path string) {
	var loaded Config
	if _, err := toml.DecodeFile(path, &loaded); err != nil {
		return
	}
	if loaded.Editor != "" {
		cfg.Editor = loaded.Editor
	}
}

// userConfigPath returns $XDG_CONFIG_HOME/tt/config.toml (defaulting to
// ~/.config/tt/config.toml), or "" if no home is known.
func userConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "tt", "config.toml")
}

// projectConfigPath searches startDir and its ancestors for a .tt.toml file.
func projectConfigPath(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".tt.toml")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// presetFor maps a bare $EDITOR/$VISUAL value to a line-aware command template.
// editorEnv may include flags (e.g. "code -w"); the binary's base name selects
// the preset and the full value is used as the command prefix.
func presetFor(editorEnv string) string {
	editorEnv = strings.TrimSpace(editorEnv)
	if editorEnv == "" {
		return ""
	}
	fields := strings.Fields(editorEnv)
	base := filepath.Base(fields[0])
	switch base {
	case "code", "code-insiders", "cursor", "codium", "vscodium":
		return editorEnv + " -g {file}:{line}"
	case "vim", "nvim", "vi", "view":
		return editorEnv + " +{line} {file}"
	case "nano":
		return editorEnv + " +{line} {file}"
	case "subl", "sublime_text":
		return editorEnv + " {file}:{line}"
	case "emacs", "emacsclient":
		return editorEnv + " +{line}:{col} {file}"
	case "idea", "goland", "pycharm", "webstorm", "clion", "rubymine", "phpstorm":
		return editorEnv + " --line {line} {file}"
	case "hx", "helix", "micro":
		return editorEnv + " {file}:{line}"
	default:
		return editorEnv + " {file}"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
