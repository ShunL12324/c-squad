package squad

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.invalid"},
		{"commit", "--allow-empty", "-m", "base"},
	} {
		if _, e := git(dir, args...); e != nil {
			t.Fatal(e)
		}
	}
}

func commitFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := git(dir, "add", name); e != nil {
		t.Fatal(e)
	}
	if _, e := git(dir, "commit", "-m", "work in "+name); e != nil {
		t.Fatal(e)
	}
	sha, e := git(dir, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	return sha
}

// Reproduces GitHub issue #1: the member works in repository B while the task
// worktree is bound to repository A, so the candidate SHA never resolves and the
// task can never leave in_progress.
func TestCrossRepositoryCodeTaskIsStuck(t *testing.T) {
	st := testStore(t)
	s, e := st.read()
	if e != nil {
		t.Fatal(e)
	}
	repoA := s.Root
	initRepo(t, repoA)
	repoB := t.TempDir()
	initRepo(t, repoB)
	if e := st.update(func(s *State) error { s.Members["a"].Cwd = repoB; return nil }); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "master", []string{"create", "cross repo work"}, options{"code": "true", "acceptance": "done"}); e != nil {
		t.Fatal(e)
	}
	s, _ = st.read()
	var id string
	for _, v := range s.Tasks {
		id = v.ID
	}
	if e := taskCommand(st, "master", []string{"assign", id}, options{"owner": "a", "to": "a,b"}); e != nil {
		t.Fatal(e)
	}
	s, _ = st.read()
	if !strings.HasPrefix(s.Tasks[id].Workspace, st.Dir) {
		t.Fatalf("workspace %q not under team dir", s.Tasks[id].Workspace)
	}

	// The real work happens in repository B and is committed there.
	shaB := commitFile(t, repoB, "feature.txt", "work\n")

	e = taskCommand(st, "a", []string{"submit", id}, options{"summary": "done", "sha": shaB})
	if e == nil {
		t.Fatal("submit accepted a SHA from a foreign repository")
	}
	t.Logf("submit error: %v", e)
	// The failure must name where the SHA was resolved and what the task is bound to.
	for _, want := range []string{shaB, s.Tasks[id].Workspace, repoA, s.Tasks[id].Target, "close-external", "Needed a single revision"} {
		if !strings.Contains(e.Error(), want) {
			t.Errorf("submit error %q does not mention %q", e, want)
		}
	}
	e = taskCommand(st, "master", []string{"approve", id}, options{})
	if e == nil {
		t.Fatal("approve accepted an unsubmitted task")
	}
	t.Logf("approve error: %v", e)
	s, _ = st.read()
	if s.Tasks[id].State != TaskPhaseInProgress {
		t.Fatalf("state = %s, want in_progress", s.Tasks[id].State)
	}
	// The owner's single unfinished-task slot stays consumed forever.
	if e := canOwn(s, &Task{ID: "other"}, "a"); e == nil {
		t.Fatal("owner slot unexpectedly free")
	} else {
		t.Logf("owner slot: %v", e)
	}
}

func captureNotices(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := notices
	notices = buf
	t.Cleanup(func() { notices = previous })
	return buf
}

// crossRepoTeam builds the issue scenario: team repository A, member "a" starting
// in repository B, and an assigned code task bound to A.
func crossRepoTeam(t *testing.T) (st *Store, id, repoA, repoB string) {
	t.Helper()
	st = testStore(t)
	s, e := st.read()
	if e != nil {
		t.Fatal(e)
	}
	repoA = s.Root
	initRepo(t, repoA)
	repoB = t.TempDir()
	initRepo(t, repoB)
	if e := st.update(func(s *State) error { s.Members["a"].Cwd = repoB; return nil }); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "master", []string{"create", "cross repo work"}, options{"code": "true", "acceptance": "done"}); e != nil {
		t.Fatal(e)
	}
	s, _ = st.read()
	for _, v := range s.Tasks {
		id = v.ID
	}
	if e := taskCommand(st, "master", []string{"assign", id}, options{"owner": "a", "to": "a,b"}); e != nil {
		t.Fatal(e)
	}
	return st, id, repoA, repoB
}

func TestCrossRepositoryTaskClosesExternally(t *testing.T) {
	st, id, repoA, repoB := crossRepoTeam(t)
	shaB := commitFile(t, repoB, "feature.txt", "work\n")
	before, e := git(repoA, "rev-parse", "refs/heads/main")
	if e != nil {
		t.Fatal(e)
	}
	countBefore, e := git(repoA, "rev-list", "--count", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	buf := captureNotices(t)
	e = taskCommand(st, "master", []string{"close-external", id}, options{
		"repo": repoB, "sha": shaB, "reason": "work was pushed from the member repository", "summary": "feature landed in repo B",
	})
	if e != nil {
		t.Fatal(e)
	}
	s, _ := st.read()
	task := s.Tasks[id]
	if task.State != TaskPhaseDone {
		t.Fatalf("state = %s, want done", task.State)
	}
	if task.MergeCommit != "" {
		t.Fatalf("merge commit recorded for an external closure: %q", task.MergeCommit)
	}
	if task.Approval != nil || task.MergeIntent != nil {
		t.Fatal("approval or merge intent left on an externally closed task")
	}
	c := task.ExternalClosure
	if c == nil {
		t.Fatal("no external closure recorded")
	}
	if c.SHA != shaB || c.By != "master" || c.Reason == "" || c.Limits == "" {
		t.Fatalf("closure = %+v", c)
	}
	if !strings.Contains(c.Limits, "no merge into") || !strings.Contains(c.Limits, "no C-Squad review or test evidence") {
		t.Fatalf("limits do not state what was unverified: %q", c.Limits)
	}
	if c.Subject != "work in feature.txt" {
		t.Fatalf("subject = %q", c.Subject)
	}
	// Hard constraints: repository A gains no commit and no foreign object.
	after, _ := git(repoA, "rev-parse", "refs/heads/main")
	if after != before {
		t.Fatalf("target branch moved: %s -> %s", before, after)
	}
	countAfter, _ := git(repoA, "rev-list", "--count", "HEAD")
	if countAfter != countBefore {
		t.Fatalf("commit count changed: %s -> %s", countBefore, countAfter)
	}
	if _, e = git(repoA, "cat-file", "-e", shaB); e == nil {
		t.Fatal("repository A now contains the repository B commit object")
	}
	// The unmerged worktree is preserved for the operator, not silently removed.
	if _, e = git(task.Workspace, "rev-parse", "HEAD"); e != nil {
		t.Fatalf("task worktree no longer usable: %v", e)
	}
	// The owner's single unfinished-task slot is free again.
	if e = canOwn(s, &Task{ID: "other"}, "a"); e != nil {
		t.Fatalf("owner still blocked: %v", e)
	}
	audited := false
	for _, ev := range s.Events {
		if ev.Kind == "closed_external" && strings.Contains(ev.Text, id) {
			audited = true
		}
	}
	if !audited {
		t.Fatal("no closed_external audit event")
	}
	told := 0
	for _, m := range s.Messages {
		if m.Task == id && strings.Contains(m.Text, "without a merge") {
			told++
		}
	}
	if told != len(task.Participants) {
		t.Fatalf("notified %d participants, want %d", told, len(task.Participants))
	}
	if !strings.Contains(buf.String(), "NOT merged") {
		t.Fatalf("closure not surfaced on stderr: %q", buf.String())
	}
	// task inspect must surface the record, not just report state done.
	buf.Reset()
	if e = taskCommand(st, "master", []string{"inspect", id}, options{}); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"NOT merged", short(shaB), repoB, "work was pushed from the member repository", "Verification limits"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("inspect output %q does not mention %q", buf.String(), want)
		}
	}
}

func TestCloseExternalRefusals(t *testing.T) {
	st, id, repoA, repoB := crossRepoTeam(t)
	captureNotices(t)
	shaB := commitFile(t, repoB, "feature.txt", "work\n")
	s, _ := st.read()
	workspace := s.Tasks[id].Workspace
	base := options{"repo": repoB, "sha": shaB, "reason": "external work"}
	with := func(key, value string) options {
		o := options{}
		for k, v := range base {
			o[k] = v
		}
		if value == "" {
			delete(o, key)
		} else {
			o[key] = value
		}
		return o
	}
	cases := []struct {
		name string
		o    options
	}{
		{"same repository", with("repo", repoA)},
		{"task worktree of the same repository", with("repo", workspace)},
		{"missing reason", with("reason", "")},
		{"missing sha", with("sha", "")},
		{"missing repo", with("repo", "")},
		{"unknown commit", with("sha", "0000000000000000000000000000000000000000")},
		{"not a repository", with("repo", t.TempDir())},
		{"missing directory", with("repo", filepath.Join(t.TempDir(), "absent"))},
	}
	for _, tc := range cases {
		if e := taskCommand(st, "master", []string{"close-external", id}, tc.o); e == nil {
			t.Errorf("%s: accepted", tc.name)
		} else {
			t.Logf("%s: %v", tc.name, e)
		}
	}
	if e := taskCommand(st, "a", []string{"close-external", id}, base); e == nil {
		t.Fatal("non-master closed a task externally")
	}
	s, _ = st.read()
	if s.Tasks[id].State != TaskPhaseInProgress || s.Tasks[id].ExternalClosure != nil {
		t.Fatalf("refused closures mutated the task: %+v", s.Tasks[id])
	}
}

// A candidate that resolves in the task worktree must go through review and the
// fast-forward merge; close-external may not quietly replace them.
func TestCloseExternalRefusesResolvableCandidate(t *testing.T) {
	st, id, _, repoB := crossRepoTeam(t)
	captureNotices(t)
	shaB := commitFile(t, repoB, "feature.txt", "work\n")
	s, _ := st.read()
	commitFile(t, s.Tasks[id].Workspace, "inside.txt", "real work\n")
	if e := taskCommand(st, "a", []string{"submit", id}, options{"summary": "done"}); e != nil {
		t.Fatal(e)
	}
	o := options{"repo": repoB, "sha": shaB, "reason": "external work"}
	if e := taskCommand(st, "master", []string{"close-external", id}, o); e == nil {
		t.Fatal("externally closed a task with a resolvable candidate")
	} else {
		t.Logf("refused: %v", e)
	}
	if e := taskCommand(st, "master", []string{"reopen", id}, options{}); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "master", []string{"close-external", id}, o); e != nil {
		t.Fatalf("still refused after an audited reopen: %v", e)
	}
}

func TestCloseExternalRejectsTaskWithoutWorkspace(t *testing.T) {
	st := testStore(t)
	captureNotices(t)
	repoB := t.TempDir()
	initRepo(t, repoB)
	sha := commitFile(t, repoB, "feature.txt", "work\n")
	if e := st.update(func(s *State) error {
		s.Tasks["T9"] = &Task{ID: "T9", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a"}}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "master", []string{"close-external", "T9"}, options{"repo": repoB, "sha": sha, "reason": "no workspace"}); e == nil {
		t.Fatal("closed a task that has no workspace")
	}
}

func TestCrossRepositoryWarningAtCreateAndAssign(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	repoA := s.Root
	initRepo(t, repoA)
	repoB := t.TempDir()
	initRepo(t, repoB)
	if e := st.update(func(s *State) error { s.Members["a"].Cwd = repoB; s.Members["b"].Cwd = repoA; return nil }); e != nil {
		t.Fatal(e)
	}
	buf := captureNotices(t)
	if e := taskCommand(st, "master", []string{"create", "cross repo work"}, options{"code": "true", "acceptance": "done"}); e != nil {
		t.Fatal(e)
	}
	s, _ = st.read()
	var id string
	for _, v := range s.Tasks {
		id = v.ID
	}
	created := buf.String()
	for _, want := range []string{"do not inherit a member --cwd", repoA, "target branch main", "member a starts in " + repoB} {
		if !strings.Contains(created, want) {
			t.Errorf("create notice %q does not mention %q", created, want)
		}
	}
	if strings.Contains(created, "member b starts in") {
		t.Errorf("warned about a member in the team repository: %q", created)
	}
	buf.Reset()
	if e := taskCommand(st, "master", []string{"assign", id}, options{"owner": "a", "to": "a,b"}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(buf.String(), "member a starts in "+repoB) {
		t.Errorf("assign did not warn: %q", buf.String())
	}
	if strings.Contains(buf.String(), "member b starts in") {
		t.Errorf("assign warned about a member in the team repository: %q", buf.String())
	}
	s, _ = st.read()
	warned, events := false, 0
	for _, m := range s.Messages {
		if m.To == "a" && strings.Contains(m.Text, "WARNING: member a starts in "+repoB) {
			warned = true
		}
		if m.To == "b" && strings.Contains(m.Text, "WARNING") {
			t.Errorf("same-repository member warned in its assignment: %q", m.Text)
		}
	}
	for _, ev := range s.Events {
		if ev.Kind == "cross_repo_member" {
			events++
		}
	}
	if !warned {
		t.Error("assignment message carries no cross-repository warning")
	}
	if events != 2 {
		t.Errorf("cross_repo_member events = %d, want 2 (create and assign)", events)
	}
}

func TestPanelDistinguishesExternalClosureFromMerge(t *testing.T) {
	st, id, _, repoB := crossRepoTeam(t)
	captureNotices(t)
	sha := commitFile(t, repoB, "feature.txt", "work\n")
	if e := taskCommand(st, "master", []string{"close-external", id}, options{"repo": repoB, "sha": sha, "reason": "external work"}); e != nil {
		t.Fatal(e)
	}
	snapshot, e := st.panelSnapshot()
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range snapshot.Tasks {
		if task.ID != id {
			continue
		}
		if !strings.Contains(task.Note, "not merged") {
			t.Errorf("card note = %q", task.Note)
		}
		for _, want := range []string{"NOT merged", short(sha), "Verification limits"} {
			if !strings.Contains(task.Detail, want) {
				t.Errorf("detail %q does not mention %q", task.Detail, want)
			}
		}
		return
	}
	t.Fatal("task missing from panel snapshot")
}
