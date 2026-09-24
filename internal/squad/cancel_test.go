package squad

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cancelStore holds T1 owned by a with b participating, in the given phase.
func cancelStore(t *testing.T, phase TaskPhase) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", Title: "Wire the panel", Owner: "a", State: phase, Updated: now(), Dispatch: DispatchModeAssigned,
			Participants: []string{"a", "b"}, Milestones: []Milestone{{Name: "plan", Gate: true, State: MilestoneStatePending}}, Evidence: []Evidence{}, Dependencies: []string{}}
		return nil
	}))
	return st
}

func readTask(t *testing.T, st *Store, id string) *Task {
	t.Helper()
	s, err := st.read()
	must(t, err)
	return s.Tasks[id]
}

// Only master cancels, and only with a reason; the reason, actor and time are
// recorded and nothing the task accumulated is discarded.
func TestCancelRequiresMasterAndReason(t *testing.T) {
	for _, phase := range []TaskPhase{TaskPhaseReady, TaskPhaseInProgress, TaskPhaseInReview, TaskPhaseAwaitingMerge} {
		t.Run(string(phase), func(t *testing.T) {
			st := cancelStore(t, phase)
			must(t, st.update(func(s *State) error {
				task := s.Tasks["T1"]
				task.Workspace, task.Branch, task.Candidate, task.Submission = "/w", "csquad/T1", "abc", "T1-r1"
				task.Evidence = []Evidence{{Member: "b", Kind: EvidenceReview, Submission: "T1-r1", Passed: true, Summary: "ok"}}
				if phase == TaskPhaseAwaitingMerge {
					task.Approval = &Approval{SHA: "abc", TargetSHA: "def", By: "master"}
				}
				return nil
			}))
			if err := taskCommand(st, "a", []string{"cancel", "T1"}, options{"reason": "no longer needed"}); err == nil || !strings.Contains(err.Error(), "only master") {
				t.Fatalf("member cancelled: %v", err)
			}
			for _, reason := range []string{"", "   "} {
				if err := taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": reason}); err == nil || !strings.Contains(err.Error(), "--reason") {
					t.Fatalf("reason %q accepted: %v", reason, err)
				}
			}
			if readTask(t, st, "T1").State != phase {
				t.Fatal("a refused cancel changed the task")
			}
			must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": " superseded by T9 "}))
			task := readTask(t, st, "T1")
			c := task.Cancellation
			if task.State != TaskPhaseCancelled || c == nil || c.Reason != "superseded by T9" || c.By != "master" || c.At == "" {
				t.Fatalf("cancellation not recorded: %+v %+v", task.State, c)
			}
			if task.Owner != "a" || task.Workspace != "/w" || task.Branch != "csquad/T1" || task.Candidate != "abc" || len(task.Evidence) != 1 || task.Approval != nil {
				t.Fatalf("history lost or approval kept: %+v", task)
			}
			if len(task.Blockers) != 0 {
				t.Fatalf("a cancelled task reports blockers: %v", task.Blockers)
			}
		})
	}
}

// Preparation and merging work on the filesystem outside the ledger, and a
// finished task is final, so none of them can be cancelled.
func TestCancelRefusesPendingFilesystemWorkAndFinishedTasks(t *testing.T) {
	for phase, want := range map[TaskPhase]string{
		TaskPhasePreparing: "abort-merge", TaskPhaseMerging: "abort-merge",
		TaskPhaseDone: "already done", TaskPhaseCancelled: "already cancelled",
	} {
		st := cancelStore(t, phase)
		before := taskJSON(t, st, "T1")
		if err := taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "stop"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", phase, err)
		}
		if taskJSON(t, st, "T1") != before {
			t.Fatalf("%s: refused cancel changed the task", phase)
		}
	}
}

// Nothing changes a cancelled task afterwards, including the operations that
// would turn it into a success.
func TestCancelledTaskIsImmutable(t *testing.T) {
	st := cancelStore(t, TaskPhaseInReview)
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "stop"}))
	before := taskJSON(t, st, "T1")
	for _, op := range []struct {
		actor string
		args  []string
		o     options
	}{
		{"master", []string{"assign", "T1"}, options{"owner": "b"}},
		{"b", []string{"claim", "T1"}, options{}},
		{"a", []string{"progress", "T1"}, options{"text": "still going"}},
		{"a", []string{"milestone", "T1"}, options{"name": "plan"}},
		{"a", []string{"submit", "T1"}, options{"summary": "done"}},
		{"b", []string{"evidence", "T1"}, options{"kind": "review", "passed": "true", "summary": "ok", "submission": "T1-r1"}},
		{"master", []string{"approve", "T1"}, options{}},
		{"master", []string{"gate", "T1"}, options{"name": "plan"}},
		{"master", []string{"reopen", "T1"}, options{}},
		{"master", []string{"close-external", "T1"}, options{"repo": "/r", "sha": "abc", "reason": "x"}},
		{"master", []string{"cancel", "T1"}, options{"reason": "again"}},
	} {
		if err := taskCommand(st, op.actor, op.args, op.o); err == nil {
			t.Fatalf("%v accepted on a cancelled task", op.args)
		}
	}
	if err := mergeCommand(st, "master", "T1"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("merge accepted: %v", err)
	}
	if err := helpCommand(st, "a", []string{"help", "request"}, options{"text": "now what?", "task": "T1"}); err == nil {
		t.Fatal("a question was opened on a cancelled task")
	}
	if taskJSON(t, st, "T1") != before {
		t.Fatal("a refused operation changed the cancelled task")
	}
	if _, err := cleanTaskWorkspace(st, "master", "T1", true); err != nil {
		t.Fatal(err)
	}
	if taskJSON(t, st, "T1") != before {
		t.Fatal("clean-worktree changed the cancelled task")
	}
}

// A cancelled task is not a finished dependency: dependents stay blocked and
// master is told which ones. Its owner is free to take other work, and its
// workspace stays on disk for clean-worktree to refuse.
func TestCancelKeepsDependentsBlockedAndReleasesTheOwner(t *testing.T) {
	st := cancelStore(t, TaskPhaseInProgress)
	workspace := t.TempDir()
	must(t, os.WriteFile(filepath.Join(workspace, "work.txt"), []byte("kept"), 0600))
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].Workspace = workspace
		s.Tasks["T2"] = &Task{ID: "T2", Title: "After", State: TaskPhaseReady, Dispatch: DispatchModeOpen, Dependencies: []string{"T1"}, Participants: []string{}, Milestones: []Milestone{}, Evidence: []Evidence{}}
		s.Tasks["T3"] = &Task{ID: "T3", Title: "Other", State: TaskPhaseReady, Dispatch: DispatchModeAssigned, Dependencies: []string{}, Participants: []string{}, Milestones: []Milestone{}, Evidence: []Evidence{}}
		return nil
	}))
	if err := taskCommand(st, "master", []string{"assign", "T3"}, options{"owner": "a"}); err == nil {
		t.Fatal("fixture: a could own a second unfinished task")
	}
	var notice bytes.Buffer
	previous := notices
	notices = &notice
	t.Cleanup(func() { notices = previous })
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "dropped"}))
	if !strings.Contains(notice.String(), "T2 stays blocked on cancelled T1") {
		t.Fatalf("dependents not named: %q", notice.String())
	}
	if blockers := readTask(t, st, "T2").Blockers; !slices.Contains(blockers, "dependency:T1") {
		t.Fatalf("dependent unblocked: %v", blockers)
	}
	if err := taskCommand(st, "b", []string{"claim", "T2"}, options{}); err == nil {
		t.Fatal("a task depending on a cancelled one was claimed")
	}
	must(t, taskCommand(st, "master", []string{"assign", "T3"}, options{"owner": "a"}))
	if data, err := os.ReadFile(filepath.Join(workspace, "work.txt")); err != nil || string(data) != "kept" {
		t.Fatalf("workspace not kept: %q %v", data, err)
	}
	if plan, err := cleanTaskWorkspace(st, "master", "T1", false); err != nil || plan.Status != "retained" {
		t.Fatalf("clean-worktree removed a cancelled workspace: %+v %v", plan, err)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatal(err)
	}
}

// Nothing queued about a cancelled task still asks anyone to act: reports,
// recovery and stall notices and undelivered dispatch notices are superseded,
// its open questions close and release their asker, and each member on it is
// told once to stop. Messages people wrote are left alone.
func TestCancelWithdrawsPendingNotices(t *testing.T) {
	st := cancelStore(t, TaskPhaseInProgress)
	var ids map[string]string
	must(t, st.update(func(s *State) error {
		task := s.Tasks["T1"]
		task.Milestones[0].State = MilestoneStateAwaitingApproval
		ids = map[string]string{
			"assigned": s.message("master", "b", "T1", assignedNotice(task), "").ID,
			"personal": s.message("a", "b", "T1", "please look at the parser", "").ID,
		}
		m := s.message("a", "master", "T1", "", "")
		m.Report = &ReportReference{Kind: "milestone", Milestone: "plan"}
		ids["milestone"] = m.ID
		s.recoveryNotice(task)
		ids["recovery"] = s.Messages[len(s.Messages)-1].ID
		m = s.message("runtime", "master", "T1", stallText(s, task), "")
		m.RequestKey = stallKey(s, task)
		m.Report = &ReportReference{Kind: "stall"}
		ids["stall"] = m.ID
		q := &Question{ID: s.next("Q"), Member: "b", Task: "T1", Text: "which parser?", State: QuestionStateOpen}
		s.Questions[q.ID] = q
		s.Members["b"].State = MemberStateWaitingMaster
		m = s.message("b", "master", "T1", "Help request "+q.ID, "")
		m.Report = &ReportReference{Kind: "decision", Question: q.ID}
		ids["question"] = m.ID
		ids["qid"] = q.ID
		return nil
	}))
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "dropped"}))
	s, err := st.read()
	must(t, err)
	byID := map[string]*Message{}
	for _, m := range s.Messages {
		byID[m.ID] = m
	}
	for _, kind := range []string{"assigned", "milestone", "recovery", "stall", "question"} {
		if m := byID[ids[kind]]; m.State != DeliveryStateSuperseded {
			t.Fatalf("%s notice still %s", kind, m.State)
		}
	}
	if m := byID[ids["personal"]]; m.State == DeliveryStateSuperseded {
		t.Fatal("a member's own message was withdrawn")
	}
	if q := s.Questions[ids["qid"]]; q.State != QuestionStateAnswered || !strings.Contains(q.Answer, "cancelled") {
		t.Fatalf("question left open: %+v", q)
	}
	if s.Members["b"].State != MemberStateIdle {
		t.Fatalf("asker still %s", s.Members["b"].State)
	}
	told := map[string]int{}
	for _, m := range s.Messages {
		if strings.HasPrefix(m.Text, "Task T1 was cancelled by master: dropped") {
			told[m.To]++
		}
	}
	if told["a"] != 1 || told["b"] != 1 || told["master"] != 0 {
		t.Fatalf("cancel notices: %v", told)
	}
	if stallEligible(s, s.Tasks["T1"], nil) {
		t.Fatal("a cancelled task can still stall")
	}
	s.queueRecoveryNotices()
	for _, m := range s.Messages {
		if m.Report != nil && m.Report.Kind == "recovery" && m.State == DeliveryStatePending {
			t.Fatal("recovery re-engaged a cancelled task")
		}
	}
}

// A cancelled task leaves its members' cards, is sorted with finished work and
// never shows the completion mark; its card says it was cancelled.
func TestCancelledTaskInPanelSnapshot(t *testing.T) {
	st := cancelStore(t, TaskPhaseInProgress)
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "dropped"}))
	must(t, st.update(func(s *State) error {
		s.Tasks["T2"] = &Task{ID: "T2", Title: "Live", Owner: "a", State: TaskPhaseInProgress, Participants: []string{"a"}, Dependencies: []string{}, Milestones: []Milestone{}, Evidence: []Evidence{}}
		return nil
	}))
	snap, err := st.panelSnapshot()
	must(t, err)
	for _, m := range snap.Members {
		if strings.Contains(m.Tasks, "T1") {
			t.Fatalf("member %s still lists cancelled T1: %q", m.ID, m.Tasks)
		}
	}
	if snap.Tasks[0].ID != "T2" || snap.Tasks[1].ID != "T1" {
		t.Fatalf("cancelled task not sorted after active work: %+v", snap.Tasks)
	}
	cancelled := snap.Tasks[1]
	if cancelled.State != "cancelled" || cancelled.Completion != "" || !strings.Contains(cancelled.Note, "Cancelled · dropped") || !strings.Contains(cancelled.Detail, "Cancelled by master") {
		t.Fatalf("cancelled card: %+v", cancelled)
	}
}

// A ledger holding a cancelled task reloads unchanged, and one written before
// cancellation existed has no such field and loads as before.
func TestCancellationRoundTrips(t *testing.T) {
	st := cancelStore(t, TaskPhaseInProgress)
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "dropped"}))
	var raw map[string]any
	must(t, json.Unmarshal([]byte(ledgerJSON(t, st)), &raw))
	task := raw["tasks"].(map[string]any)["T1"].(map[string]any)
	if task["state"] != "cancelled" || task["cancellation"].(map[string]any)["reason"] != "dropped" {
		t.Fatalf("stored task: %v", task)
	}
	old := cancelStore(t, TaskPhaseDone)
	if strings.Contains(ledgerJSON(t, old), "cancellation") {
		t.Fatal("an uncancelled task stores a cancellation field")
	}
}
