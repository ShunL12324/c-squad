// Package preflight checks executable availability before runtime mutations.
// It does not install software, start engines, or inspect account credentials.
package preflight

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/ShunL12324/c-squad/internal/config"
)

// ErrMissingDependency allows callers to distinguish setup failures from task failures.
var ErrMissingDependency = errors.New("required executable unavailable")

// Check requires tmux, ps, and the selected engine. An empty engine requires at least
// one supported engine, as used by installation checks. Git is task-specific.
func Check(engine config.Engine) error {
	var failures []error
	for _, name := range []string{"tmux", "ps"} {
		if err := executable(name); err != nil {
			failures = append(failures, err)
		}
	}
	if engine != "" {
		if err := engine.Validate(); err != nil {
			return err
		}
		if err := executable(string(engine)); err != nil {
			failures = append(failures, err)
		}
	} else {
		_, claudeErr := exec.LookPath(string(config.Claude))
		_, codexErr := exec.LookPath(string(config.Codex))
		if claudeErr != nil && codexErr != nil {
			failures = append(failures, fmt.Errorf("install Claude Code or Codex and add it to PATH: %w", ErrMissingDependency))
		}
	}
	return errors.Join(failures...)
}

// Git checks the optional dependency required for code tasks and worktrees.
func Git() error { return executable("git") }
func executable(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("install %s and ensure it is on PATH: %w: %w", name, ErrMissingDependency, err)
	}
	return nil
}
