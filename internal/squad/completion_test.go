package squad

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotCompletion(t *testing.T, st *Store, id string) string {
	t.Helper()
	snap, err := st.panelSnapshot()
	must(t, err)
	for _, task := range snap.Tasks {
		if task.ID == id {
			return task.Completion
		}
	}
	t.Fatalf("task %s missing from snapshot", id)
	return ""
}

// A code task is marked complete only once master merged the reviewed and
// tested candidate; every earlier phase, including approval, stays unmarked.
func TestCodeTaskCompletionFollowsMerge(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	root := s.Root
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, e := git(root, args...); e != nil {
			t.Fatal(e)
		}
	}
	must(t, taskCommand(st, "master", []string{"create", "change"}, options{"code": "true", "acceptance": "tests pass"}))
	s, _ = st.read()
	var task *Task
	for _, v := range s.Tasks {
		task = v
	}
	id, wt := task.ID, task.Workspace
	must(t, os.WriteFile(filepath.Join(wt, "hello.txt"), []byte("hello\n"), 0o600))
	for _, args := range [][]string{{"add", "hello.txt"}, {"commit", "-m", "change"}} {
		if _, e := git(wt, args...); e != nil {
			t.Fatal(e)
		}
	}
	must(t, st.update(func(s *State) error {
		t := s.Tasks[id]
		t.Owner, t.State, t.Participants = "a", TaskPhaseInProgress, []string{"a", "b"}
		return nil
	}))
	unmarked := func(step string) {
		t.Helper()
		if got := snapshotCompletion(t, st, id); got != "" {
			t.Fatalf("%s: marked %q", step, got)
		}
	}
	unmarked("in progress")
	must(t, taskCommand(st, "a", []string{"submit", id}, options{"summary": "done"}))
	unmarked("in review, author's claim only")
	sha, _ := git(wt, "rev-parse", "HEAD")
	must(t, taskCommand(st, "b", []string{"evidence", id}, options{"kind": "review", "sha": sha, "passed": "true", "summary": "verified"}))
	if e := taskCommand(st, "master", []string{"approve", id}, options{}); e == nil {
		t.Fatal("approved without test evidence")
	}
	must(t, taskCommand(st, "b", []string{"evidence", id}, options{"kind": "test", "sha": sha, "passed": "true", "summary": "verified"}))
	unmarked("passing evidence, not approved")
	must(t, taskCommand(st, "master", []string{"approve", id}, options{}))
	unmarked("approved, not merged")
	must(t, taskCommand(st, "master", []string{"merge", id}, options{}))
	s, _ = st.read()
	if s.Tasks[id].State != TaskPhaseDone || s.Tasks[id].MergeCommit == "" {
		t.Fatalf("merge did not finish: %+v", s.Tasks[id])
	}
	if got := snapshotCompletion(t, st, id); got != "✓ Completed · merged "+short(s.Tasks[id].MergeCommit) {
		t.Fatalf("merged task not marked: %q", got)
	}
}

func TestNonCodeTaskCompletionFollowsApproval(t *testing.T) {
	st := submissionStore(t)
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "findings"}))
	sub := currentSubmission(t, st).Submission
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "false", "summary": "missing source"}))
	if e := taskCommand(st, "master", []string{"approve", "T1"}, options{}); e == nil {
		t.Fatal("approved over failing evidence")
	}
	if got := snapshotCompletion(t, st, "T1"); got != "" {
		t.Fatalf("rejected research marked %q", got)
	}
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "true", "summary": "sources checked"}))
	must(t, taskCommand(st, "master", []string{"approve", "T1"}, options{}))
	if got := snapshotCompletion(t, st, "T1"); got != "✓ Completed · accepted by master" {
		t.Fatalf("accepted research not marked: %q", got)
	}
}

func TestCompletionIgnoresUnacceptedStates(t *testing.T) {
	cases := map[string]Task{
		"blocked":          {State: TaskPhaseBlocked, Blockers: []string{"needs a key"}},
		"in review":        {State: TaskPhaseInReview, Submission: "T1-r1"},
		"awaiting merge":   {State: TaskPhaseAwaitingMerge, Workspace: "/w"},
		"merging":          {State: TaskPhaseMerging, Workspace: "/w"},
		"blocker on ready": {State: TaskPhaseReady, Blockers: []string{"x"}},
		"done unmerged":    {State: TaskPhaseDone, Workspace: "/w"},
		"closed external":  {State: TaskPhaseDone, Workspace: "/w", ExternalClosure: &ExternalClosure{SHA: "abc"}},
		"legacy click on unfinished work": {State: TaskPhaseInReview,
			UserConfirmation: &UserConfirmation{At: "2026-09-23T00:00:00Z", Actor: UserSender, Phase: TaskPhaseDone}},
	}
	for name, task := range cases {
		if got := completion(&task); got != "" {
			t.Errorf("%s: marked %q", name, got)
		}
	}
}

// Ledgers written while the panel asked for a user click still load, keep the
// record for audit, and derive completion from the workflow alone.
func TestLegacyUserConfirmationLoads(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	var raw map[string]any
	must(t, json.Unmarshal([]byte(ledgerJSON(t, st)), &raw))
	task := raw["tasks"].(map[string]any)["T1"].(map[string]any)
	task["state"] = "done"
	task["user_confirmation"] = map[string]any{"at": "2026-09-23T00:00:00Z", "actor": UserSender, "phase": "done"}
	data, err := json.Marshal(raw)
	must(t, err)
	_, err = st.DB.Exec("UPDATE state SET data=? WHERE id=1", string(data))
	must(t, err)
	s, err := st.read()
	must(t, err)
	if c := s.Tasks["T1"].UserConfirmation; c == nil || c.Actor != UserSender {
		t.Fatalf("legacy record lost: %+v", c)
	}
	snap, err := st.panelSnapshot()
	must(t, err)
	if !strings.Contains(snap.Tasks[0].Detail, "User confirmation (legacy record): 2026-09-23T00:00:00Z") {
		t.Fatalf("legacy record not shown: %s", snap.Tasks[0].Detail)
	}
	// Done, but with a workspace and no merge: the old click does not count.
	must(t, st.update(func(s *State) error { s.Tasks["T1"].Workspace = "/w"; return nil }))
	if got := snapshotCompletion(t, st, "T1"); got != "" {
		t.Fatalf("legacy click marked completion: %q", got)
	}
	must(t, st.update(func(s *State) error { return nil }))
	if !strings.Contains(ledgerJSON(t, st), `"user_confirmation"`) {
		t.Fatal("rewrite dropped the legacy record")
	}
}

func TestTaskConfirmIsGone(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	before := taskJSON(t, st, "T1")
	if err := taskCommand(st, "master", []string{"confirm", "T1"}, options{}); err == nil {
		t.Fatal("task confirm still accepted")
	}
	if before != taskJSON(t, st, "T1") {
		t.Fatal("rejected confirm mutated the task")
	}
}
