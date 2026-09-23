package squad

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStartCollisionIsReadOnlyBeforePreflight(t *testing.T) {
	for _, phase := range []TeamPhase{TeamPhaseRunning, TeamPhaseStopped, TeamPhaseInterrupted} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("CSQUAD_HOME", t.TempDir())
			st := namedTeamStore(t, projectBase(root), "existing")
			must(t, st.update(func(s *State) error { s.Root = root; s.Phase = phase; s.Active = phase == TeamPhaseRunning; return nil }))
			before, err := st.read()
			must(t, err)
			t.Setenv("PATH", t.TempDir()) // Collision must win even when no engine can launch.
			err = start(options{"name": "existing", "detach": "true"})
			if err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("want collision, got %v", err)
			}
			after, err := st.read()
			must(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected start mutated saved team")
			}
			if _, err = os.Stat(filepath.Join(root, ".git", "info", "exclude")); !os.IsNotExist(err) {
				t.Fatalf("start reached project setup: %v", err)
			}
		})
	}
}

func TestLegacyStartCollisionIsReadOnly(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CSQUAD_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := namedTeamStore(t, stateBase(), "legacy")
	must(t, st.update(func(s *State) error { s.Root = root; return nil }))
	before, err := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, err)
	err = start(options{"name": "legacy"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("legacy collision: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("legacy ledger changed")
	}
	if _, err = os.Stat(projectBase(root)); !os.IsNotExist(err) {
		t.Fatalf("created local state on collision: %v", err)
	}
}

func TestNameSelectionAndCompletionShareBinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CSQUAD_HOME", home)
	bound := namedTeamStore(t, home, "bound")
	other := namedTeamStore(t, home, "other")
	dir, err := ResolveTeamDirectory("", "other")
	must(t, err)
	if dir != other.Dir {
		t.Fatalf("selected %s", dir)
	}
	values, err := CompletionValues(dir, "member")
	must(t, err)
	if !reflect.DeepEqual(values, []string{"master"}) {
		t.Fatal(values)
	}
	bindSession(t, bound, "master", "1")
	for _, path := range [][]string{{"task", "list"}, {"help", "list"}, {"member", "attach", "master"}, {"ui"}} {
		err := Execute(path, options{"team-name": "other"}, nil)
		if err == nil || !strings.Contains(err.Error(), "bound to team") {
			t.Fatalf("%v escaped: %v", path, err)
		}
	}
	if _, err := ResolveTeamDirectory("", "other"); err == nil {
		t.Fatal("completion escaped binding")
	}
	if _, err := CompletionValues(other.Dir, "member"); err == nil {
		t.Fatal("direct completion escaped binding")
	}
	if _, err := ResolveTeamDirectory("", "missing"); err == nil {
		t.Fatal("unknown name silently fell back")
	}
}

func TestTableOutputEscapesRowsAndSortsFields(t *testing.T) {
	var out bytes.Buffer
	must(t, writeTable(&out, map[string]any{"z": "line\nnext\tvalue", "a": map[string]string{"id": "T1"}}))
	text := out.String()
	if strings.Count(text, "\n") != 3 || !strings.Contains(text, `line\nnext\tvalue`) || strings.Index(text, `"a"`) > strings.Index(text, `"z"`) {
		t.Fatalf("invalid table: %s", text)
	}
}

func TestResourceTablesAreConciseRows(t *testing.T) {
	for _, tc := range []struct {
		value           any
		header, content string
	}{
		{map[string]*Member{"alice": {ID: "alice", State: MemberStateIdle, Instructions: "Review the candidate"}}, "MEMBER", "alice"},
		{map[string]*Task{"T1": {ID: "T1", Title: "Check result", Owner: "alice"}}, "OWNER", "Check result"},
		{map[string]*Question{"Q1": {ID: "Q1", Member: "alice", Text: "Which target?"}}, "QUESTION", "Which target?"},
		{[]*Message{{ID: "M1", From: "alice", To: "master", Text: "line\nnext"}}, "FROM", "line\\nnext"},
	} {
		var out bytes.Buffer
		must(t, writeTable(&out, tc.value))
		text := out.String()
		if !strings.Contains(text, tc.header) || !strings.Contains(text, tc.content) || strings.Contains(text, "FIELD") || strings.Count(text, "\n") != 2 {
			t.Fatalf("not a resource row: %s", text)
		}
	}
}

func TestIntegratedRemovalDispatch(t *testing.T) {
	st, _ := savedRemovalTeam(t, false)
	s, err := st.read()
	must(t, err)
	t.Chdir(s.Root)
	t.Setenv("CSQUAD_HOME", "")
	must(t, Execute([]string{"team", "remove"}, options{"name": "old", "dry-run": "true"}, nil))
	if _, err = os.Stat(filepath.Join(st.Dir, "state.db")); err != nil {
		t.Fatal("dry run removed ledger", err)
	}
	bindSession(t, st, "master", "1")
	if err = Execute([]string{"team", "remove"}, options{"name": "old", "dry-run": "true"}, nil); err == nil || !strings.Contains(err.Error(), "outside the team") {
		t.Fatalf("bound removal reached wrong dispatch: %v", err)
	}
}

func TestPromptUsesCanonicalCLIAndSubmissionIdentity(t *testing.T) {
	st := testStore(t)
	s, err := st.read()
	must(t, err)
	text, promptErr := prompt(s, s.Members["a"])
	must(t, promptErr)
	for _, want := range []string{"csquad COMMAND", "message reply MESSAGE", "question request", "--submission ID", "Non-code evidence requires --submission", "Routine messages do not require message ack"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	// Answering escalations is master's command; a worker is not shown it.
	master, promptErr := prompt(s, s.Members["master"])
	must(t, promptErr)
	if !strings.Contains(master, "question answer") || strings.Contains(text, "question answer") {
		t.Fatal("question answer must be listed for master only")
	}
	if strings.Contains(text, "help request") || strings.Contains(master, "help request") {
		t.Fatal("prompt retained old escalation spelling")
	}
}
