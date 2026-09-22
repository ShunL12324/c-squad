package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Load falls back to the developer's own configuration when CSQUAD_CONFIG is
// unset, and a load now migrates a legacy file and writes the result back. Every
// test sets the variable, but pointing it at a temporary file here as well means
// a test added without it still cannot reach the real config.toml.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "csquad-config-suite-")
	if err != nil {
		panic(err)
	}
	if err = os.Setenv("CSQUAD_CONFIG", filepath.Join(dir, "config.toml")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
