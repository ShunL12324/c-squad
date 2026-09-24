package squad

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Cancellation records why master stopped a task. A cancelled task is final:
// it satisfies no dependency and shows no completion mark, and everything it
// accumulated (owner, workspace, candidate, evidence) stays as history.
type Cancellation struct {
	Reason string `json:"reason"`
	By     string `json:"by"`
	At     string `json:"at"`
}

// terminal reports a phase no task operation may change. Only done is a
// successful finish; code asking whether work succeeded must test for done.
func (p TaskPhase) terminal() bool {
	return p == TaskPhaseDone || p == TaskPhaseCancelled
}

// availableNotice and assignedNotice are the dispatch notices a member receives
// for a task. Cancelling a task withdraws the ones not delivered yet, which it
// recognises by these prefixes.
func availableNotice(t *Task) string {
	return "Task available: " + t.ID + " " + t.Title + ". Run task inspect " + t.ID + " and claim if suitable."
}

func assignedNotice(t *Task) string {
	return "Assigned to task " + t.ID + ". Run task inspect " + t.ID + " for workspace, ownership, acceptance and milestones; use board for cross-task coordination."
}

func dispatchNotice(m *Message, t *Task) bool {
	return m.From == "master" && m.Task == t.ID &&
		(strings.HasPrefix(m.Text, "Task available: "+t.ID+" ") || strings.HasPrefix(m.Text, "Assigned to task "+t.ID+". ") || strings.HasPrefix(m.RequestKey, ccKeyPrefix+t.ID+":"))
}

// cancelTask moves a task to cancelled inside the caller's transaction and
// returns the tasks left blocked on it. Preparation and merging touch the
// filesystem outside the ledger, so they must finish or be aborted first.
func cancelTask(s *State, actor string, t *Task, reason string) ([]string, error) {
	if actor != "master" {
		return nil, fmt.Errorf("only master may cancel a task: %w", ErrMasterRequired)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, errors.New("--reason required")
	}
	switch t.State {
	case TaskPhaseDone, TaskPhaseCancelled:
		return nil, fmt.Errorf("task is already %s", t.State)
	case TaskPhasePreparing, TaskPhaseMerging:
		return nil, fmt.Errorf("task is %s; wait for it to finish, or run reconcile or task abort-merge, before cancelling", t.State)
	}
	at := now()
	t.State = TaskPhaseCancelled
	t.Cancellation = &Cancellation{Reason: reason, By: actor, At: at}
	t.Approval = nil
	t.Blockers = []string{}

	notify := []string{}
	if t.Owner != "" {
		notify = append(notify, t.Owner)
	}
	for _, id := range t.Participants {
		if !slices.Contains(notify, id) {
			notify = append(notify, id)
		}
	}
	// A question about a cancelled task no longer needs an answer. Closing it
	// releases an asker held in waiting_master and drops its decision report.
	text := "Task " + t.ID + " was cancelled by " + actor + ": " + reason
	for _, q := range s.Questions {
		if q.Task != t.ID || q.State != QuestionStateOpen {
			continue
		}
		q.Answer = text
		q.State = QuestionStateAnswered
		if !slices.Contains(notify, q.Member) {
			notify = append(notify, q.Member)
		}
		if m := s.Members[q.Member]; m != nil && m.State == MemberStateWaitingMaster {
			m.State = MemberStateIdle
			for _, other := range s.Questions {
				if other.Member == q.Member && other.State == QuestionStateOpen {
					m.State = MemberStateWaitingMaster
				}
			}
		}
	}
	for _, m := range s.Messages {
		if dispatchNotice(m, t) && m.State == DeliveryStatePending {
			m.State = DeliveryStateSuperseded
			m.Error = "task cancelled; retained for history"
		}
	}
	for _, id := range notify {
		if id == actor || id == "master" {
			continue
		}
		if _, e := s.recipient(id); e == nil {
			s.message(actor, id, t.ID, text+". Stop work on it; its workspace is kept.", "")
		}
	}
	dependents := []string{}
	for _, id := range sortedTaskIDs(s) {
		if other := s.Tasks[id]; !other.State.terminal() && slices.Contains(other.Dependencies, t.ID) {
			dependents = append(dependents, id)
		}
	}
	return dependents, nil
}
