// Package paths resolves the XDG directories csquad owns and the Claude Code
// directories it reads.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Data returns $XDG_DATA_HOME/csquad, defaulting to ~/.local/share/csquad.
func Data() (string, error) { return xdg("XDG_DATA_HOME", ".local", "share") }

// Config returns $XDG_CONFIG_HOME/csquad, defaulting to ~/.config/csquad.
func Config() (string, error) { return xdg("XDG_CONFIG_HOME", ".config") }

// ClaudeHome returns Claude Code's configuration directory: $CLAUDE_CONFIG_DIR
// or ~/.claude. Transcripts live under its projects directory.
func ClaudeHome() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// ClaudeState returns the global Claude Code state file that records workspace
// trust: $CLAUDE_CONFIG_DIR/.claude.json or ~/.claude.json.
func ClaudeState() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// Canonical returns dir as an absolute path with symlinks resolved, so the same
// directory always compares equal in the queue.
func Canonical(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func xdg(env string, fallback ...string) (string, error) {
	base := os.Getenv(env)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate the home directory: %w", err)
		}
		base = filepath.Join(append([]string{home}, fallback...)...)
	}
	return filepath.Join(base, "csquad"), nil
}
