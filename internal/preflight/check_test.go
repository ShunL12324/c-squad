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
	if err := Check(config.Claude); err != nil {
		t.Fatal(err)
	}
	if err := Check(""); err != nil {
		t.Fatal(err)
	}
	if err := Check(config.Codex); !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("missing selected engine: %v", err)
	}
	if err := Git(); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Git should be missing: %v", err)
	}
	if err := Check(config.Engine("unknown")); err == nil {
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
	cfg := config.Config{EngineCommands: map[config.Engine]config.Command{
		config.Codex: {Executable: filepath.Join(dir, "renamed client"), Args: []string{"literal prefix"}},
	}}
	for _, engine := range []config.Engine{config.Codex, ""} {
		if err := CheckConfig(cfg, engine); err != nil {
			t.Fatal(err)
		}
	}
	cfg.EngineCommands[config.Codex] = config.Command{Executable: "missing-custom-client"}
	if err := CheckConfig(cfg, config.Codex); !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), "missing-custom-client") {
		t.Fatalf("missing custom executable: %v", err)
	}
}
