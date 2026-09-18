package squad

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/preflight"
)

func TestPersistedEnumsKeepWireValues(t *testing.T) {
	var s State
	raw := `{"phase":"interrupted","members":{"a":{"engine":"codex","state":"waiting_master"}},"tasks":{"T1":{"state":"in_review","dispatch":"assigned"}},"messages":[{"state":"pending"}]}`
	must(t, json.Unmarshal([]byte(raw), &s))
	if s.Phase != TeamPhaseInterrupted || s.Members["a"].Engine != config.Codex || s.Members["a"].State != MemberStateWaitingMaster || s.Tasks["T1"].State != TaskPhaseInReview || s.Tasks["T1"].Dispatch != DispatchModeAssigned || s.Messages[0].State != DeliveryStatePending {
		t.Fatal("legacy wire values changed")
	}
	out, err := json.Marshal(s)
	must(t, err)
	if !strings.Contains(string(out), `"state":"in_review"`) {
		t.Fatal("enum encoded as non-string")
	}
}

func TestCompletionReadsWithoutUpdatingTeam(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T19"] = &Task{ID: "T19", State: TaskPhaseReady}
		s.Members["b"].State = MemberStateRemoved
		return nil
	}))
	var before, after string
	must(t, st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&before))
	values, err := CompletionValues(st.Dir, "task")
	must(t, err)
	if len(values) != 1 || values[0] != "T19" {
		t.Fatal(values)
	}
	members, err := CompletionValues(st.Dir, "member")
	must(t, err)
	if contains(members, "b") {
		t.Fatal("completed removed member")
	}
	must(t, st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&after))
	if before != after {
		t.Fatal("completion modified the ledger")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	_, _ = CompletionValues(missing, "task")
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("completion created a team directory")
	}
}

func TestStartupPreflightLeavesNoTeam(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CSQUAD_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("CSQUAD_HOME", filepath.Join(root, "state"))
	err := start(options{"name": "missing-tools", "detach": "true"})
	if !errors.Is(err, preflight.ErrMissingDependency) {
		t.Fatalf("expected setup failure: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("startup created a team before checking dependencies")
	}
}

func TestDomainErrorsSurviveWrapping(t *testing.T) {
	st := testStore(t)
	s, err := st.read()
	must(t, err)
	_, err = s.member("missing")
	joined := errors.Join(fmt.Errorf("inspect: %w", err), errors.New("secondary cleanup failure"))
	if !errors.Is(joined, ErrNotFound) {
		t.Fatal("lost error classification")
	}
	st.Actor = "a"
	st.Generation = 999
	if err = st.update(func(*State) error { return nil }); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("missing stale generation category: %v", err)
	}
}
