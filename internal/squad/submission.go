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
