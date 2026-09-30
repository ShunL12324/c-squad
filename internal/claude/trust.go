package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/paths"
)

// EnsureTrusted marks dir as a trusted workspace in Claude Code's global state
// so `claude --bg` can start there without the interactive trust prompt.
//
// This writes projects[dir].hasTrustDialogAccepted in ~/.claude.json, an
// internal file rather than a supported interface. The file is replaced
// atomically, but a running Claude Code process that rewrites it at the same
// moment can still lose one side's change. Nothing is written when dir or one
// of its parents is already trusted. The home directory is refused because
// Claude Code never persists trust for it.
func EnsureTrusted(dir string) error {
	home, err := os.UserHomeDir()
	if err == nil {
		if h, e := paths.Canonical(home); e == nil && h == dir {
			return errors.New("claude does not persist trust for the home directory; queue the task in a project directory")
		}
	}
	path, err := paths.ClaudeState()
	if err != nil {
		return err
	}
	// Replace the real file, not a dotfiles symlink pointing at it.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data = []byte("{}")
	} else if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// Numbers stay json.Number so large IDs and timestamps round-trip exactly.
	dec.UseNumber()
	var state map[string]any
	if err := dec.Decode(&state); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	projects, _ := state["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
		state["projects"] = projects
	}
	for d := dir; ; d = filepath.Dir(d) {
		if entry, ok := projects[d].(map[string]any); ok && entry["hasTrustDialogAccepted"] == true {
			return nil
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	entry, _ := projects[dir].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		projects[dir] = entry
	}
	entry["hasTrustDialogAccepted"] = true
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude.json.csquad-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("mark %s trusted in %s: %w", dir, path, err)
	}
	return nil
}
