package cli

import (
	"os"
	"testing"
)

// The CLI adapter dispatches into squad.Execute, which falls back to these keys
// when a flag is absent. A member session exports them, so a suite run from
// inside a team would parse against a live identity and reach its real ledger.
// Clearing them once keeps parsing tests independent of where they run.
var hermeticKeys = []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION", "CSQUAD_HOME", "CSQUAD_CONFIG"}

func TestMain(m *testing.M) {
	for _, key := range hermeticKeys {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func TestSuiteEnvironmentIsHermetic(t *testing.T) {
	for _, key := range hermeticKeys {
		if value, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s leaked into the suite as %q; TestMain must clear it", key, value)
		}
	}
}
