package squad

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ShunL12324/c-squad/internal/preflight"
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
	var result any
	var notice string
	err := st.update(func(s *State) error {
		if p[0] == "list" {
			result = s.Tasks
			return nil
		}
		if p[0] == "create" {
			if actor != "master" {
				return fmt.Errorf("only master creates tasks: %w", ErrMasterRequired)
			}
			if len(p) < 2 || o["acceptance"] == "" {
				return errors.New("title and --acceptance required")
			}
			if st.Generation > 0 && o["request-id"] == "" {
				return errors.New("agents must provide a stable --request-id when creating a task")
			}
			if o["request-id"] != "" {
				for _, existing := range s.Tasks {
					if existing.RequestKey == actor+":"+o["request-id"] {
						if existing.Title != strings.Join(p[1:], " ") || existing.Acceptance != o["acceptance"] || existing.Description != o["description"] || (existing.Workspace != "") != (o["code"] == "true") {
							return errors.New("request-id already used for different task content")
						}
						result = existing
						return nil
					}
				}
			}
			dispatch := DispatchMode(o["dispatch"])
			if dispatch == "" {
				dispatch = DispatchModeAssigned
			}
			if dispatch != DispatchModeAssigned && dispatch != DispatchModeOpen {
				return errors.New("--dispatch assigned|open required")
			}
			t := &Task{Dispatch: dispatch, Setup: o["setup"], ID: s.next("T"), Title: strings.Join(p[1:], " "), Description: o["description"], Acceptance: o["acceptance"], State: TaskPhaseReady, Updated: now(), Participants: []string{}, Dependencies: list(o["deps"]), Milestones: []Milestone{}, Evidence: []Evidence{}}
			if o["request-id"] != "" {
				t.RequestKey = actor + ":" + o["request-id"]
			}
			for _, d := range t.Dependencies {
				if s.Tasks[d] == nil {
					return fmt.Errorf("unknown dependency %s", d)
				}
			}
			names := list(o["milestones"])
			for _, gate := range list(o["gates"]) {
				if !contains(names, gate) {
					return fmt.Errorf("gate %s is not a milestone", gate)
				}
			}
			seen := map[string]bool{}
			for _, name := range names {
				if seen[name] {
					return errors.New("duplicate milestone")
				}
				seen[name] = true
				t.Milestones = append(t.Milestones, Milestone{name, contains(list(o["gates"]), name), MilestoneStatePending})
			}
			if o["code"] == "true" {
				if err := preflight.Git(); err != nil {
					return err
				}
				branch, e := git(s.Root, "symbolic-ref", "--short", "HEAD")
				if e != nil {
					return errors.New("code tasks require a Git branch with a commit")
				}
				base, e := git(s.Root, "rev-parse", "HEAD")
				if e != nil {
					return e
				}
				t.Base = base
				t.Target = branch
				t.Branch = "csquad/" + s.ID + "/" + t.ID
				t.Workspace = filepath.Join(st.Dir, "worktrees", t.ID)
				// Persist the workspace intent before invoking Git.
				t.State = TaskPhasePreparing
				printNotice([]string{codeTaskBinding(s, t)})
				noticeCrossRepoTeam(s, actor, t)
			}
			s.Tasks[t.ID] = t
			s.event(actor, "task_created", t.ID+" "+t.Title)
			for id, m := range s.Members {
				if t.State == TaskPhaseReady && t.Dispatch == DispatchModeOpen && id != "master" && m.State != MemberStateRemoved {
					s.message(actor, id, t.ID, "Task available: "+t.ID+" "+t.Title+". Read board and claim if suitable.", "")
				}
			}
			result = t
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
		if op != "claim" && !masterOnly && actor != "master" && !contains(t.Participants, actor) {
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
			if !contains(members, owner) {
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
				newlyAssigned := t.Owner == "" || !contains(t.Participants, id)
				if !contains(t.Participants, id) {
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
			notice = "Claimed task " + t.ID
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
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("unknown milestone: %w", ErrNotFound)
			}
			notice = "Milestone " + o["name"] + " reached for " + t.ID
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
			t.Candidate = ""
			t.CandidateAuthor = ""
			t.Evidence = nil
			for _, id := range t.Participants {
				s.message(actor, id, t.ID, "Candidate withdrawn; stop review/testing. Owner may edit and resubmit.", "")
			}
		case "submit":
			if t.State != TaskPhaseInProgress && t.State != TaskPhaseInReview {
				return errors.New("task must be in progress or review to submit")
			}
			if t.Owner != actor && actor != "master" {
				return errors.New("only task owner submits candidate")
			}
			for _, m := range t.Milestones {
				if m.Gate && m.State != MilestoneStateApproved {
					return fmt.Errorf("gate %s not approved", m.Name)
				}
			}
			if o["summary"] == "" {
				return errors.New("--summary required")
			}
			if t.Workspace != "" {
				sha := o["sha"]
				if sha == "" {
					sha = "HEAD"
				}
				candidate, e := git(t.Workspace, "rev-parse", "--verify", sha+"^{commit}")
				if e != nil {
					// Name both repositories: a bare "Needed a single revision" hides
					// that the SHA was resolved against the task worktree, not the
					// member's own working directory.
					return fmt.Errorf("candidate %q does not resolve in the task worktree %s; %s. If the commit lives in another repository, master can close this task with task close-external --repo PATH --sha COMMIT --reason TEXT: %w",
						sha, t.Workspace, codeTaskBinding(s, t), e)
				}
				head, e := git(t.Workspace, "rev-parse", "HEAD")
				if e != nil {
					return e
				}
				if candidate != head {
					return errors.New("candidate must be current task HEAD")
				}
				dirty, e := git(t.Workspace, "status", "--porcelain")
				if e != nil {
					return e
				}
				if dirty != "" {
					return errors.New("commit all task changes before submit")
				}
				if t.State == TaskPhaseInReview && t.Candidate != candidate {
					return errors.New("candidate frozen; master must task reopen before changing it")
				}
				t.Candidate = candidate
			}
			if t.CandidateAuthor == "" {
				t.CandidateAuthor = t.Owner
			}
			t.Progress = o["summary"]
			t.State = TaskPhaseInReview
			t.Approval = nil
			notice = "Task submitted: " + t.ID + " " + t.Progress + " candidate=" + t.Candidate
		case "close-external":
			if e = closeExternal(s, actor, t, o); e != nil {
				return e
			}
			printNotice(describeExternalClosure(t.ExternalClosure))
		case "evidence":
			if EvidenceKind(o["kind"]) != EvidenceReview && EvidenceKind(o["kind"]) != EvidenceTest {
				return errors.New("--kind review|test required")
			}
			if t.State != TaskPhaseInReview && t.State != TaskPhaseAwaitingMerge {
				return errors.New("submit candidate before evidence")
			}
			if o["sha"] != t.Candidate {
				return errors.New("evidence SHA must match candidate")
			}
			if o["summary"] == "" {
				return errors.New("--summary required")
			}
			if o["passed"] != "true" && o["passed"] != "false" {
				return errors.New("--passed true|false required")
			}
			if EvidenceKind(o["kind"]) == EvidenceReview && (actor == t.Owner || actor == t.CandidateAuthor) {
				return errors.New("author cannot review own candidate")
			}
			t.Evidence = append(t.Evidence, Evidence{actor, EvidenceKind(o["kind"]), t.Candidate, o["passed"] == "true", o["summary"]})
			t.Approval = nil
			t.State = TaskPhaseInReview
			notice = "Evidence recorded: " + t.ID + " " + o["kind"] + " passed=" + o["passed"]
		case "approve":
			if t.State != TaskPhaseInReview {
				return errors.New("task not submitted for review")
			}
			if t.Workspace == "" {
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
		if notice != "" && actor != "master" {
			s.message(actor, "master", t.ID, notice, "")
		}
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
	return jsonOut(result)
}
func checkEvidence(t *Task) error {
	latest := map[string]Evidence{}
	for _, ev := range t.Evidence {
		if ev.SHA == t.Candidate {
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
	if !seen[EvidenceReview] || !seen[EvidenceTest] {
		return errors.New("passing review and test evidence for candidate required")
	}
	return nil
}
