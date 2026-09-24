package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
)

// hermeticKeys are exported into every process a member session spawns, so a
// suite started from inside a team inherits a live identity. Execute would then
// resolve the running team's directory and act as that member, writing its real
// ledger. Clearing the keys once, before any test runs, keeps the suite bound to
// its temporary stores; subprocess helpers inherit the cleaned copy through
// os.Environ. Tests that need a value set it back with t.Setenv.
var hermeticKeys = []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION", "CSQUAD_HOME", "CSQUAD_CONFIG", "CSQUAD_VERSIONS_DIR", forwardedEnv}

// CSQUAD_CONFIG is cleared like the others and then pointed at a temporary file:
// config.Load falls back to the real user configuration when it is unset, and
// loading now migrates legacy files and writes the result back. A suite left on
// the fallback would rewrite the developer's own config.toml.
func TestMain(m *testing.M) {
	// start and resume pin the running build and run the copy with "version" to
	// confirm it; in this suite the running build is the test binary.
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(buildinfo.String())
		os.Exit(0)
	}
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
	// Pinned copies go to the suite's directory, never the user's data home.
	if err = os.Setenv("CSQUAD_VERSIONS_DIR", filepath.Join(dir, "versions")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestSuiteEnvironmentIsHermetic(t *testing.T) {
	for _, key := range hermeticKeys {
		if key == "CSQUAD_CONFIG" || key == "CSQUAD_VERSIONS_DIR" {
			continue
		}
		if value, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s leaked into the suite as %q; TestMain must clear it", key, value)
		}
	}
	// An unset CSQUAD_CONFIG resolves to the real user file, which config.Load
	// may rewrite. The suite must never reach it, not even to read it.
	path := os.Getenv("CSQUAD_CONFIG")
	if !strings.HasPrefix(path, os.TempDir()) {
		t.Fatalf("CSQUAD_CONFIG must point into a temporary directory; got %q", path)
	}
	// Without TestMain this command would adopt the surrounding member identity
	// and fail to find it in the temporary team, or worse, target the live one.
	st := testStore(t)
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir}, nil); e != nil {
		t.Fatalf("clean environment must resolve the requested team as master: %v", e)
	}
}
