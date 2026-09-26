package squad

import (
	"bytes"
	"strings"
	"testing"
)

func TestTaskTableBoundsLongFieldsAndKeepsActionableFacts(t *testing.T) {
	task := &Task{
		ID: "T7", Title: strings.Repeat("long title ", 70), State: TaskPhaseInProgress,
		Owner: "worker", Dispatch: DispatchModeAssigned, Workspace: "/tmp/task/T7",
		Description: strings.Repeat("scope ", 90), Acceptance: strings.Repeat("acceptance ", 90),
		Setup: strings.Repeat("setup ", 90), Progress: strings.Repeat("progress ", 90),
		Blockers: []string{strings.Repeat("first blocker ", 30), "second", "third", "fourth", "fifth"},
		Milestones: []Milestone{
			{Name: "later", State: MilestoneStatePending},
			{Name: "critical review", Gate: true, State: MilestoneStateAwaitingApproval},
		},
	}
	var out bytes.Buffer
	if err := writeTable(&out, task); err != nil {
		t.Fatal(err)
	}
	view := out.String()
	for _, want := range []string{"T7", "owner worker", "Workspace: /tmp/task/T7", "Scope:", "Acceptance:", "Blockers: 5", "+1 more blockers", "Gates: 1 awaiting approval", "critical review [gate] awaiting_approval", "Next: resolve recorded blockers", "Full record: csquad task inspect T7 --output json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q from task summary:\n%s", want, view)
		}
	}
	if len(view) > 2300 || strings.Contains(view, "\nsecond\n") {
		t.Fatalf("task summary is unbounded or malformed (%d bytes):\n%s", len(view), view)
	}
}

func TestTaskTableUsesOnlyLatestEvidenceForCurrentSubmission(t *testing.T) {
	task := &Task{
		ID: "T8", State: TaskPhaseInReview, Submission: "T8-r2", Candidate: "current",
		Evidence: []Evidence{
			{Member: "reviewer", Kind: EvidenceReview, Submission: "T8-r1", SHA: "old", Passed: false, Summary: "stale failure"},
			{Member: "reviewer", Kind: EvidenceReview, Submission: "T8-r2", SHA: "current", Passed: true, Summary: "first pass"},
			{Member: "reviewer", Kind: EvidenceReview, Submission: "T8-r2", SHA: "current", Passed: false, Summary: "current issue"},
			{Member: "tester", Kind: EvidenceTest, Submission: "T8-r2", SHA: "different", Passed: false, Summary: "wrong commit"},
			{Member: "tester", Kind: EvidenceTest, Submission: "T8-r2", SHA: "current", Passed: true, Summary: "race passes"},
		},
	}
	var out bytes.Buffer
	if err := writeTable(&out, task); err != nil {
		t.Fatal(err)
	}
	view := out.String()
	for _, want := range []string{"Submission: T8-r2 | candidate current", "Evidence: 2 current latest; 1 failing; 5 total records", "#3 FAIL review reviewer: current issue", "#5 PASS test tester: race passes", "Next: resolve current failing evidence"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q from current evidence:\n%s", want, view)
		}
	}
	for _, stale := range []string{"stale failure", "first pass", "wrong commit"} {
		if strings.Contains(view, stale) {
			t.Fatalf("stale evidence %q included:\n%s", stale, view)
		}
	}
}

func TestTaskTableCountsHiddenFailures(t *testing.T) {
	task := &Task{ID: "T9", State: TaskPhaseInReview, Submission: "T9-r1"}
	for i := range 12 {
		task.Evidence = append(task.Evidence, Evidence{Member: string(rune('a' + i)), Kind: EvidenceReview, Submission: "T9-r1", Passed: false, Summary: "failed"})
	}
	var out bytes.Buffer
	if err := writeTable(&out, task); err != nil {
		t.Fatal(err)
	}
	view := out.String()
	for _, want := range []string{"Evidence: 12 current latest; 12 failing; 12 total records", "+4 more current evidence entries (including 4 unshown failures)", "Full record: csquad task inspect T9 --output json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q from bounded failures:\n%s", want, view)
		}
	}
}
