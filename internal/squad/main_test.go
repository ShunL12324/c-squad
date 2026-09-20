package squad

import (
	"os"
	"testing"
)

// hermeticKeys are exported into every process a member session spawns, so a
// suite started from inside a team inherits a live identity. Execute would then
// resolve the running team's directory and act as that member, writing its real
// ledger. Clearing the keys once, before any test runs, keeps the suite bound to
// its temporary stores; subprocess helpers inherit the cleaned copy through
// os.Environ. Tests that need a value set it back with t.Setenv.
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
	// Without TestMain this command would adopt the surrounding member identity
	// and fail to find it in the temporary team, or worse, target the live one.
	st := testStore(t)
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir}, nil); e != nil {
		t.Fatalf("clean environment must resolve the requested team as master: %v", e)
	}
}
