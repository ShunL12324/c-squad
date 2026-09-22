// Package preflight checks executable availability before runtime mutations.
// It does not install software, start engines, or inspect account credentials.
package preflight

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

// ErrMissingDependency allows callers to distinguish setup failures from task failures.
var ErrMissingDependency = errors.New("required executable unavailable")

// Check requires tmux, ps and at least one supported engine, as used by an
// installation check. Each engine is probed through the profile that would
// launch it, so a renamed or wrapped client counts as installed. Git is
// task-specific and checked separately.
func Check(cfg config.Config) error {
	failures := baseTools()
	claudeCommand, claudeEnv := cfg.LaunchFor(config.Claude)
	codexCommand, codexEnv := cfg.LaunchFor(config.Codex)
	if EngineCommand(claudeCommand, claudeEnv) != nil && EngineCommand(codexCommand, codexEnv) != nil {
		failures = append(failures, fmt.Errorf("install Claude Code or Codex and add it to PATH: %w", ErrMissingDependency))
	}
	return errors.Join(failures...)
}

// CheckCommand checks the command a member will actually launch, without
// executing it. Launch settings all come from a profile now, so the caller
// resolves the command and environment and passes them in; checking anything
// else would pass while the real launch fails.
func CheckCommand(engine config.Engine, command config.Command, env map[string]string) error {
	if err := engine.Validate(); err != nil {
		return err
	}
	failures := baseTools()
	if err := EngineCommand(command, env); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// baseTools checks the dependencies every check shares.
func baseTools() []error {
	var failures []error
	for _, name := range []string{"tmux", "ps"} {
		if err := executable(name); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// EngineCommand checks direct executables or a persistent alias in its shell.
func EngineCommand(command config.Command, env map[string]string) error {
	if command.Shell == "" {
		return executable(command.Executable)
	}
	name, args, prepared := command.ProbeInvocation(env)
	if _, err := process.RunEnv("", prepared, name, args...); err != nil {
		return fmt.Errorf("engine command %q unavailable in interactive %s: %w: %w", command.Executable, command.Shell, ErrMissingDependency, err)
	}
	return nil
}

// Git checks the optional dependency required for code tasks and worktrees.
func Git() error { return executable("git") }
func executable(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("install %s and ensure it is on PATH: %w: %w", name, ErrMissingDependency, err)
	}
	return nil
}
