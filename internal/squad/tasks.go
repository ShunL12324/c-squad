package squad

import (
	"errors"
	"fmt"
	"slices"
)

func taskCommand(st *Store, actor string, p []string, o options) error {
	if len(p) == 0 {
		return errors.New("task operation required")
	}
	if len(p) > 1 && p[0] == "abort-merge" {
		return abortMerge(st, actor, p[1])
	}
	if len(p) > 1 && p[0] == "merge" {
		return mergeCommand(st, actor, p[1])
	}
	// Asking about a task is not an operation on it. Routed here so the phase
	// rejection below, which exists to stop edits to finished work, cannot take
	// the button away from exactly the done tasks the user wants explained.
	if len(p) > 1 && p[0] == "brief" {
		return briefCommand(st, actor, p[1])
	}
	if len(p) > 1 && p[0] == "clean-worktree" {
		plan, err := cleanTaskWorkspace(st, actor, p[1], o["dry-run"] == "true")
		if err != nil {
			return err
		}
		return jsonOut(plan)
	}
	var result any
	var report *ReportReference
	err := st.update(func(s *State) error {
		if p[0] == "list" {
			result = s.Tasks
			return nil
		}
		if p[0] == "create" {
			task, err := st.createTask(s, actor, p, o)
			if err != nil {
				return err
			}
			result = task
			return nil
		}
		if len(p) < 2 {
			return errors.New("task ID required")
		}
		t, e := s.task(p[1])
		if e != nil {
			return e
		}
		op := p[0]
		if op == "inspect" {
			printNotice(describeExternalClosure(t.ExternalClosure))
			result = t
			return nil
		}
		if t.State == TaskPhaseDone || t.State == TaskPhaseMerging || t.State == TaskPhasePreparing {
			return errors.New("task is completed or has a pending filesystem operation")
		}
		masterOnly := op == "assign" || op == "approve" || op == "merge" || op == "gate" || op == "reopen" || op == "close-external"
		if masterOnly && actor != "master" {
			return fmt.Errorf("only master may perform this task operation: %w", ErrMasterRequired)
		}
		if op == "claim" || op == "assign" {
			for _, d := range t.Dependencies {
				if s.Tasks[d].State != TaskPhaseDone {
					return fmt.Errorf("dependency %s not done", d)
				}
			}
		}
		if op != "claim" && !masterOnly && actor != "master" && !slices.Contains(t.Participants, actor) {
			return errors.New("not a participant in this task")
		}
		if (op == "claim" || op == "submit" || op == "approve") && len(t.Blockers) > 0 {
			return fmt.Errorf("task blocked: %v", t.Blockers)
		}
		switch op {
		case "assign":
			owner := o["owner"]
			if owner == "" {
				owner = t.Owner
			}
			if owner == "" {
				return errors.New("--owner required; participants do not imply ownership")
			}
			if t.Owner != "" && t.Owner != owner {
				return errors.New("remove old owner before handoff; preserve workspace and phase")
			}
			if e = canOwn(s, t, owner); e != nil {
				return e
			}
			members := list(o["to"])
			if !slices.Contains(members, owner) {
				members = append(members, owner)
			}
			if len(members) == 0 {
				return errors.New("--to required")
			}
			for _, id := range members {
				m, e := s.member(id)
				if e != nil {
					return e
				}
				if m.State == MemberStateRemoved {
					return errors.New("member removed")
				}
				newlyAssigned := t.Owner == "" || !slices.Contains(t.Participants, id)
				if !slices.Contains(t.Participants, id) {
					t.Participants = append(t.Participants, id)
				}
				crossRepo := noticeCrossRepo(s, actor, t, m)
				if newlyAssigned {
					text := "Assigned to task " + t.ID + ". Read board for workspace, ownership, acceptance and milestones."
					if crossRepo != "" {
						text += " WARNING: " + crossRepo
					}
					s.message(actor, id, t.ID, text, "")
				}
			}
			t.Owner = owner
			if t.State == TaskPhaseReady {
				t.State = TaskPhaseInProgress
			}
		case "claim":
			if t.Dispatch != DispatchModeOpen || t.State != TaskPhaseReady || t.Owner != "" {
				return errors.New("task already claimed or not ready")
			}
			if e = canOwn(s, t, actor); e != nil {
				return e
			}
			t.Owner = actor
			t.Participants = append(t.Participants, actor)
			t.State = TaskPhaseInProgress
			// Ownership is recorded on the task; no master interruption.
		case "progress":
			if o["text"] == "" {
				return errors.New("--text required")
			}
			t.Progress = o["text"]
			s.event(actor, "progress", t.ID+": "+t.Progress)
		case "milestone":
			found := false
			for i := range t.Milestones {
				m := &t.Milestones[i]
				if m.Name == o["name"] {
					if m.State != MilestoneStatePending {
						return errors.New("milestone already reported")
					}
					m.State = MilestoneStateReported
					if m.Gate {
						m.State = MilestoneStateAwaitingApproval
						report = &ReportReference{Kind: "decision", Milestone: m.Name}
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("unknown milestone: %w", ErrNotFound)
			}
			// Ordinary milestones remain visible in the ledger and UI.
		case "gate":
			found := false
			for i := range t.Milestones {
				m := &t.Milestones[i]
				if m.Name == o["name"] && m.State == MilestoneStateAwaitingApproval {
					m.State = MilestoneStateApproved
					found = true
				}
			}
			if !found {
				return errors.New("no pending gate with this name")
			}

			for _, id := range t.Participants {
				s.message(actor, id, t.ID, "Reporting gate approved: "+o["name"], "")
			}
		case "reopen":
			if t.State != TaskPhaseInReview && t.State != TaskPhaseAwaitingMerge {
				return errors.New("only submitted tasks may reopen")
			}
			t.State = TaskPhaseInProgress
			t.Approval = nil
			t.Submission = ""
			t.SubmissionSummary = ""
			t.Candidate = ""
			t.CandidateAuthor = ""
			t.Evidence = nil
			for _, id := range t.Participants {
				s.message(actor, id, t.ID, "Candidate withdrawn; stop review/testing. Owner may edit and resubmit.", "")
			}
		case "submit":
			report, e = submitTask(s, actor, t, o)
			if e != nil {
				return e
			}
		case "close-external":
			if e = closeExternal(s, actor, t, o); e != nil {
				return e
			}
			printNotice(describeExternalClosure(t.ExternalClosure))
		case "evidence":
			report, e = recordTaskEvidence(actor, t, o)
			if e != nil {
				return e
			}
		case "approve":
			if t.State != TaskPhaseInReview {
				return errors.New("task not submitted for review")
			}
			if t.Workspace == "" {
				if e = checkEvidence(t); e != nil {
					return e
				}
				t.State = TaskPhaseDone
				break
			}
			if e = checkEvidence(t); e != nil {
				return e
			}
			head, e := git(t.Workspace, "rev-parse", "HEAD")
			if e != nil {
				return e
			}
			if head != t.Candidate {
				return errors.New("candidate changed; resubmit")
			}
			target, e := git(s.Root, "rev-parse", "refs/heads/"+t.Target)
			if e != nil {
				return e
			}
			t.Approval = &Approval{t.Candidate, target, actor}
			t.State = TaskPhaseAwaitingMerge
		default:
			return errors.New("unknown task operation")
		}
		t.Updated = now()
		if report != nil && actor != "master" {
			s.taskReport(actor, t, report)
		}
		s.expireReports()
		s.event(actor, op, t.ID)
		result = t
		return nil
	})
	if err != nil {
		return err
	}
	if p[0] == "create" {
		if task, ok := result.(*Task); ok && task.State == TaskPhasePreparing {
			if err = st.prepareWorkspace(task.ID); err != nil {
				return err
			}
			cur, err := st.read()
			if err != nil {
				return err
			}
			result = cur.Tasks[task.ID]
		}
	}
	// The durable outbox retains failed deliveries for runtime retry.
	_ = st.syncMessages()
	return queryOut(o, result)
}
func checkEvidence(t *Task) error {
	latest := map[string]Evidence{}
	for _, ev := range t.Evidence {
		if t.Submission != "" && ev.Submission == t.Submission && ev.SHA == t.Candidate {
			latest[ev.Member+":"+string(ev.Kind)] = ev
		}
	}
	seen := map[EvidenceKind]bool{}
	for _, ev := range latest {
		if !ev.Passed {
			return fmt.Errorf("unresolved failing %s evidence from %s", ev.Kind, ev.Member)
		}
		seen[ev.Kind] = true
	}
	if t.Workspace != "" && (!seen[EvidenceReview] || !seen[EvidenceTest]) {
		return errors.New("passing review and test evidence for candidate required")
	}
	return nil
}
