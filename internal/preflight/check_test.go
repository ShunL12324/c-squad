package preflight

import (
	"errors"
	"os"
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
