package preflight

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

func TestSelectedEngineAndOptionalGit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, tool := range []string{"tmux", "ps", "claude"} {
		if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	claude := config.EngineCommand(config.Claude)
	if err := CheckCommand(config.Claude, claude, nil); err != nil {
		t.Fatal(err)
	}
	// One installed engine satisfies the installation check.
	if err := Check(config.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := CheckCommand(config.Codex, config.EngineCommand(config.Codex), nil); !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("missing selected engine: %v", err)
	}
	if err := Git(); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Git should be missing: %v", err)
	}
	if err := CheckCommand(config.Engine("unknown"), claude, nil); err == nil {
		t.Fatal("accepted unknown engine")
	}
}

func TestMissingAliasDoesNotFallBack(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip(shell + " unavailable")
			}
			home := t.TempDir()
			command := config.Command{Shell: shell, Executable: "nonexistent_csquad_alias"}
			err := EngineCommand(command, map[string]string{"HOME": home, "ZDOTDIR": home})
			if !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), command.Executable) || !strings.Contains(err.Error(), shell) {
				t.Fatalf("alias diagnostic: %v", err)
			}
		})
	}
}

func TestConfiguredEngineWithoutNativeName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, tool := range []string{"tmux", "ps", "renamed client"} {
		if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The caller resolves the launch command from a profile, so the check probes
	// the renamed executable a member would really start, not the engine name.
	profile := config.Profile{Engine: config.Codex, Command: &config.Command{
		Executable: filepath.Join(dir, "renamed client"), Args: []string{"literal prefix"}}}
	if err := CheckCommand(config.Codex, profile.LaunchCommand(config.Codex), profile.Env); err != nil {
		t.Fatal(err)
	}
	// The installation check probes the profiles rather than the native names, so
	// a configuration that only ever launches the renamed client still passes.
	cfg := config.Config{Profiles: map[string]config.Profile{"renamed": profile}, DefaultProfile: "renamed"}
	if err := Check(cfg); err != nil {
		t.Fatal(err)
	}
	missing := config.Profile{Engine: config.Codex, Command: &config.Command{Executable: "missing-custom-client"}}
	if err := CheckCommand(config.Codex, missing.LaunchCommand(config.Codex), nil); !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), "missing-custom-client") {
		t.Fatalf("missing custom executable: %v", err)
	}
}
