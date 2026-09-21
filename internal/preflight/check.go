// Package preflight checks executable availability before runtime mutations.
// It does not install software, start engines, or inspect account credentials.
package preflight

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

// ErrMissingDependency allows callers to distinguish setup failures from task failures.
var ErrMissingDependency = errors.New("required executable unavailable")

// Check requires tmux, ps, and the selected engine. An empty engine requires at least
// one supported engine, as used by installation checks. Git is task-specific.
func Check(engine config.Engine) error {
	return CheckConfig(config.Config{}, engine)
}

// CheckConfig checks configured engine executables without executing them.
func CheckConfig(cfg config.Config, engine config.Engine, environments ...map[string]string) error {
	if err := cfg.ValidateCommands(); err != nil {
		return err
	}
	var failures []error
	env := agentenv.Merge(cfg.Env, cfg.StartupEnv)
	if len(environments) > 0 {
		env = environments[0]
	}
	for _, name := range []string{"tmux", "ps"} {
		if err := executable(name); err != nil {
			failures = append(failures, err)
		}
	}
	if engine != "" {
		if err := engine.Validate(); err != nil {
			return err
		}
		if err := EngineCommand(cfg.Command(engine), env); err != nil {
			failures = append(failures, err)
		}
	} else {
		claudeErr := EngineCommand(cfg.Command(config.Claude), env)
		codexErr := EngineCommand(cfg.Command(config.Codex), env)
		if claudeErr != nil && codexErr != nil {
			failures = append(failures, fmt.Errorf("install Claude Code or Codex and add it to PATH: %w", ErrMissingDependency))
		}
	}
	return errors.Join(failures...)
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
