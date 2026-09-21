package squad

import (
	"errors"
	"fmt"
)

func submissionID(t *Task) string {
	return fmt.Sprintf("%s-r%d", t.ID, t.SubmissionRevision)
}

// Legacy ledgers had no revision identity. Only migrate once, so evidence missing
// an identity in a modern ledger can never be rebound to a later submission.
func migrateSubmission(t *Task) {
	if t.Submission != "" || t.SubmissionRevision != 0 {
		return
	}
	if t.State != TaskPhaseInReview && t.State != TaskPhaseAwaitingMerge && t.State != TaskPhaseMerging && t.State != TaskPhaseDone {
		return
	}
	if t.ExternalClosure != nil {
		return
	}
	t.SubmissionRevision = 1
	t.Submission = submissionID(t)
	t.SubmissionSummary = t.Progress
	for i := range t.Evidence {
		ev := &t.Evidence[i]
		if ev.Submission == "" && ev.SHA == t.Candidate {
			ev.Submission = t.Submission
		}
	}
}

func validateEvidenceSelector(t *Task, o options) error {
	if t.Submission == "" {
		return errors.New("submit candidate before evidence")
	}
	if o["submission"] != "" && o["submission"] != t.Submission {
		return errors.New("evidence submission must match current submission")
	}
	if t.Workspace == "" {
		if o["sha"] != "" {
			return errors.New("non-code evidence requires --submission, not --sha")
		}
		if o["submission"] == "" {
			return errors.New("--submission required for non-code evidence")
		}
		return nil
	}
	if o["sha"] == "" && o["submission"] == "" {
		return errors.New("--sha or --submission required for code evidence")
	}
	if o["sha"] != "" && o["sha"] != t.Candidate {
		return errors.New("evidence SHA must match candidate")
	}
	if t.Candidate == "" {
		return errors.New("code evidence requires a candidate commit")
	}
	return nil
}

// submitTask runs inside taskCommand's existing transaction.
func submitTask(s *State, actor string, t *Task, o options) (*ReportReference, error) {
	if t.State != TaskPhaseInProgress && t.State != TaskPhaseInReview {
		return nil, errors.New("task must be in progress or review to submit")
	}
	if t.Owner != actor && actor != "master" {
		return nil, errors.New("only task owner submits candidate")
	}
	for _, m := range t.Milestones {
		if m.Gate && m.State != MilestoneStateApproved {
			return nil, fmt.Errorf("gate %s not approved", m.Name)
		}
	}
	if o["summary"] == "" {
		return nil, errors.New("--summary required")
	}
	if t.Submission != "" && t.SubmissionSummary != o["summary"] {
		return nil, errors.New("submission frozen; master must task reopen before changing it")
	}
	if t.Workspace == "" && o["sha"] != "" {
		return nil, errors.New("non-code submissions do not accept --sha")
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
			return nil, fmt.Errorf("candidate %q does not resolve in the task worktree %s; %s. If the commit lives in another repository, master can close this task with task close-external --repo PATH --sha COMMIT --reason TEXT: %w",
				sha, t.Workspace, codeTaskBinding(s, t), e)
		}
		head, e := git(t.Workspace, "rev-parse", "HEAD")
		if e != nil {
			return nil, e
		}
		if candidate != head {
			return nil, errors.New("candidate must be current task HEAD")
		}
		dirty, e := git(t.Workspace, "status", "--porcelain")
		if e != nil {
			return nil, e
		}
		if dirty != "" {
			return nil, errors.New("commit all task changes before submit")
		}
		if t.State == TaskPhaseInReview && t.Candidate != candidate {
			return nil, errors.New("candidate frozen; master must task reopen before changing it")
		}
		t.Candidate = candidate
	}
	if t.Submission == "" {
		t.SubmissionRevision++
		t.Submission = submissionID(t)
		t.SubmissionSummary = o["summary"]
	}
	if t.CandidateAuthor == "" {
		t.CandidateAuthor = t.Owner
	}
	t.Progress = o["summary"]
	t.State = TaskPhaseInReview
	t.Approval = nil
	return &ReportReference{Kind: "delivery", Submission: t.Submission}, nil
}

// recordTaskEvidence runs inside taskCommand's existing transaction.
func recordTaskEvidence(actor string, t *Task, o options) (*ReportReference, error) {
	var report *ReportReference
	wasReady := evidenceReady(t)
	if EvidenceKind(o["kind"]) != EvidenceReview && EvidenceKind(o["kind"]) != EvidenceTest {
		return nil, errors.New("--kind review|test required")
	}
	if t.State != TaskPhaseInReview && t.State != TaskPhaseAwaitingMerge {
		return nil, errors.New("submit candidate before evidence")
	}
	if e := validateEvidenceSelector(t, o); e != nil {
		return nil, e
	}
	if o["summary"] == "" {
		return nil, errors.New("--summary required")
	}
	if o["passed"] != "true" && o["passed"] != "false" {
		return nil, errors.New("--passed true|false required")
	}
	if EvidenceKind(o["kind"]) == EvidenceReview && (actor == t.Owner || actor == t.CandidateAuthor) {
		return nil, errors.New("author cannot review own candidate")
	}
	t.Evidence = append(t.Evidence, Evidence{Member: actor, Kind: EvidenceKind(o["kind"]), SHA: t.Candidate, Submission: t.Submission, Passed: o["passed"] == "true", Summary: o["summary"]})
	t.Approval = nil
	t.State = TaskPhaseInReview
	if o["passed"] == "false" {
		report = &ReportReference{Kind: "failure", Submission: t.Submission, Evidence: len(t.Evidence)}
	} else if !wasReady && evidenceReady(t) {
		report = &ReportReference{Kind: "ready", Submission: t.Submission}
	}
	return report, nil
}
