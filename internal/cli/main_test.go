package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI adapter dispatches into squad.Execute, which falls back to these keys
// when a flag is absent. A member session exports them, so a suite run from
// inside a team would parse against a live identity and reach its real ledger.
// Clearing them once keeps parsing tests independent of where they run.
var hermeticKeys = []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION", "CSQUAD_HOME", "CSQUAD_CONFIG"}

// CSQUAD_CONFIG is then pointed at a temporary file rather than left unset:
// config.Load falls back to the real user configuration, which it migrates and
// rewrites. A suite on that fallback would edit the developer's own config.toml.
func TestMain(m *testing.M) {
	for _, key := range hermeticKeys {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	dir, err := os.MkdirTemp("", "csquad-suite-config-")
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

func TestSuiteEnvironmentIsHermetic(t *testing.T) {
	for _, key := range hermeticKeys {
		if key == "CSQUAD_CONFIG" {
			continue
		}
		if value, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s leaked into the suite as %q; TestMain must clear it", key, value)
		}
	}
	if path := os.Getenv("CSQUAD_CONFIG"); !strings.HasPrefix(path, os.TempDir()) {
		t.Fatalf("CSQUAD_CONFIG must point into a temporary directory; got %q", path)
	}
}
