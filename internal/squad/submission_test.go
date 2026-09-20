package squad

import (
	"encoding/json"
	"strings"
	"testing"
)

func submissionStore(t *testing.T) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a", "b"}}
		return nil
	}))
	return st
}

func currentSubmission(t *testing.T, st *Store) *Task {
	t.Helper()
	s, err := st.read()
	must(t, err)
	return s.Tasks["T1"]
}

func TestNonCodeSubmissionEvidenceLifecycle(t *testing.T) {
	st := submissionStore(t)
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "research result"}))
	first := currentSubmission(t, st)
	if first.Submission == "" || first.Candidate != "" {
		t.Fatalf("bad submission: %+v", first)
	}
	must(t, taskCommand(st, "a", []string{"progress", "T1"}, options{"text": "review pending"}))
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "research result"}))
	if got := currentSubmission(t, st); got.Submission != first.Submission || got.SubmissionSummary != "research result" {
		t.Fatalf("retry changed identity: %+v", got)
	}
	if err := taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "changed result"}); err == nil {
		t.Fatal("changed immutable summary")
	}
	ev := options{"submission": first.Submission, "kind": "review", "passed": "false", "summary": "missing source"}
	if err := taskCommand(st, "a", []string{"evidence", "T1"}, ev); err == nil || !strings.Contains(err.Error(), "author") {
		t.Fatalf("self review: %v", err)
	}
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, ev))
	if err := taskCommand(st, "master", []string{"approve", "T1"}, options{}); err == nil {
		t.Fatal("approved unresolved failure")
	}
	ev["passed"] = "true"
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, ev))
	must(t, taskCommand(st, "master", []string{"reopen", "T1"}, options{}))
	withdrawn := currentSubmission(t, st)
	if withdrawn.Submission != "" || len(withdrawn.Evidence) != 0 {
		t.Fatalf("reopen retained evidence: %+v", withdrawn)
	}
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "changed result"}))
	second := currentSubmission(t, st)
	if second.Submission == first.Submission || second.SubmissionRevision != 2 {
		t.Fatalf("reused identity: %+v", second)
	}
	if err := taskCommand(st, "b", []string{"evidence", "T1"}, ev); err == nil {
		t.Fatal("accepted stale evidence")
	}
	ev["submission"] = second.Submission
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, ev))
	got := currentSubmission(t, st)
	if got.Evidence[0].SHA != "" || got.Evidence[0].Submission != second.Submission {
		t.Fatalf("bad evidence: %+v", got.Evidence)
	}
	must(t, taskCommand(st, "master", []string{"approve", "T1"}, options{}))
	if currentSubmission(t, st).State != TaskPhaseDone {
		t.Fatal("not done")
	}
}

func TestNonCodeApprovalEvidenceOptional(t *testing.T) {
	st := submissionStore(t)
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "done"}))
	must(t, taskCommand(st, "master", []string{"approve", "T1"}, options{}))
}

func TestSubmissionSelectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		code bool
		opts options
		ok   bool
	}{
		{"research needs ID", false, options{}, false},
		{"research fictitious sha", false, options{"submission": "T1-r1", "sha": "abc"}, false},
		{"research ID", false, options{"submission": "T1-r1"}, true},
		{"code needs selector", true, options{}, false},
		{"code legacy sha", true, options{"sha": "abc"}, true},
		{"code ID", true, options{"submission": "T1-r1"}, true},
		{"code both", true, options{"submission": "T1-r1", "sha": "abc"}, true},
		{"code wrong sha", true, options{"submission": "T1-r1", "sha": "def"}, false},
		{"code stale ID even with sha", true, options{"submission": "T1-r0", "sha": "abc"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{Submission: "T1-r1"}
			if tc.code {
				task.Workspace = "repo"
				task.Candidate = "abc"
			}
			if err := validateEvidenceSelector(task, tc.opts); (err == nil) != tc.ok {
				t.Fatalf("error=%v, want success=%v", err, tc.ok)
			}
		})
	}
}

func TestLegacySubmissionMigration(t *testing.T) {
	var s State
	must(t, json.Unmarshal([]byte(`{"tasks":{"T1":{"id":"T1","state":"awaiting_merge","workspace":"repo","candidate":"abc","progress":"original","evidence":[{"member":"b","kind":"review","sha":"abc","passed":true},{"member":"b","kind":"test","sha":"abc","passed":true},{"member":"b","kind":"review","sha":"old","passed":false}]},"T2":{"id":"T2","state":"in_review","progress":"research"}}}`), &s))
	normalizeState(&s)
	code := s.Tasks["T1"]
	if code.Submission != "T1-r1" || code.SubmissionSummary != "original" {
		t.Fatalf("migration: %+v", code)
	}
	must(t, checkEvidence(code))
	if s.Tasks["T2"].Submission != "T2-r1" {
		t.Fatal("non-code not migrated")
	}
	code.Evidence[0].Submission = ""
	normalizeState(&s)
	if err := checkEvidence(code); err == nil {
		t.Fatal("modern unbound evidence was rebound")
	}
	code.Evidence[0].Submission = "T1-r0"
	if err := checkEvidence(code); err == nil {
		t.Fatal("stale review counted")
	}
	code.Evidence[0].Submission = code.Submission
	code.Evidence[0].SHA = "old"
	if err := checkEvidence(code); err == nil {
		t.Fatal("wrong SHA counted")
	}
}

func TestCodeSubmissionEvidenceRevisionAndApproval(t *testing.T) {
	st := submissionStore(t)
	s, err := st.read()
	must(t, err)
	initRepo(t, s.Root)
	sha := commitFile(t, s.Root, "result.txt", "result")
	must(t, st.update(func(s *State) error {
		task := s.Tasks["T1"]
		task.Workspace = s.Root
		task.Target = "main"
		return nil
	}))
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "done"}))
	first := currentSubmission(t, st).Submission
	ev := options{"submission": first, "kind": "review", "passed": "true", "summary": "verified"}
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, ev))
	if err := taskCommand(st, "master", []string{"approve", "T1"}, options{}); err == nil {
		t.Fatal("code approved without tests")
	}
	must(t, taskCommand(st, "a", []string{"evidence", "T1"}, options{"sha": sha, "kind": "test", "passed": "true", "summary": "tested"}))
	must(t, taskCommand(st, "master", []string{"approve", "T1"}, options{}))
	task := currentSubmission(t, st)
	if task.Approval.SHA != sha || task.Evidence[0].SHA != sha || task.Evidence[1].Submission != first {
		t.Fatalf("lost SHA/submission binding: %+v", task)
	}
	ev["passed"] = "false"
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, ev))
	if currentSubmission(t, st).Approval != nil {
		t.Fatal("evidence retained approval")
	}
	if err := taskCommand(st, "master", []string{"approve", "T1"}, options{}); err == nil {
		t.Fatal("failing evidence approved")
	}
	must(t, taskCommand(st, "master", []string{"reopen", "T1"}, options{}))
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "done"}))
	if currentSubmission(t, st).Submission == first {
		t.Fatal("same SHA reused submission")
	}
	if err := taskCommand(st, "b", []string{"evidence", "T1"}, ev); err == nil {
		t.Fatal("stale same-SHA submission accepted")
	}
	if err := taskCommand(st, "master", []string{"approve", "T1"}, options{}); err == nil {
		t.Fatal("prior revision evidence approved new submission")
	}
}
