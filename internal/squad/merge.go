package squad

import (
	"errors"
	"fmt"

	"github.com/ShunL12324/c-squad/internal/filelock"
)

// A durable intent bridges SQLite and Git: they cannot share a transaction.
// Once intent is written, no task mutation is permitted until reconciliation.
func mergeCommand(st *Store, actor, id string) error {
	if actor != "master" {
		return fmt.Errorf("only master may merge: %w", ErrMasterRequired)
	}
	unlock, e := filelock.Acquire(st.Dir, "merge", false)
	if e != nil {
		return e
	}
	defer unlock()
	e = st.update(func(s *State) error {
		t, e := s.task(id)
		if e != nil {
			return e
		}
		if t.State == TaskPhaseDone {
			return nil
		}
		if t.State == TaskPhaseCancelled {
			return errors.New("task was cancelled and cannot be merged")
		}
		if t.State == TaskPhaseMerging && t.MergeIntent != nil {
			return nil
		}
		if t.State != TaskPhaseAwaitingMerge || t.Approval == nil {
			return errors.New("master approval required")
		}
		if len(t.Blockers) > 0 {
			return fmt.Errorf("task blocked: %v", t.Blockers)
		}
		if e = validateMerge(s, t); e != nil {
			return e
		}
		approval := *t.Approval
		t.MergeIntent = &approval
		t.State = TaskPhaseMerging
		s.event(actor, "merge_intent", id+" "+approval.SHA)
		return nil
	})
	if e != nil {
		return e
	}
	s, e := st.read()
	if e != nil {
		return e
	}
	t := s.Tasks[id]
	if t.State == TaskPhaseDone {
		return jsonOut(t)
	}
	// On a resumed merge the ref may already have advanced; never run it twice.
	if merged(s, t) {
		return st.finishMerge(id)
	}
	if e = validateMerge(s, t); e != nil {
		return fmt.Errorf("merge intent preserved; reconcile or repair workspace: %w", e)
	}
	if _, e = git(s.Root, "merge", "--ff-only", t.MergeIntent.SHA); e != nil {
		// Keep intent even on failure: Git hooks or an interrupted command may have
		// advanced the ref. Reconciliation decides from the actual ref, not exit code.
		return e
	}
	return st.finishMerge(id)
}

func validateMerge(s *State, t *Task) error {
	if t.Approval == nil {
		return errors.New("missing approval")
	}
	if e := checkEvidence(t); e != nil {
		return e
	}
	head, e := git(t.Workspace, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	target, e := git(s.Root, "rev-parse", "refs/heads/"+t.Target)
	if e != nil {
		return e
	}
	if head != t.Approval.SHA || target != t.Approval.TargetSHA {
		return errors.New("candidate or target changed; resubmit and reapprove")
	}
	for _, dir := range []string{s.Root, t.Workspace} {
		dirty, e := git(dir, "status", "--porcelain")
		if e != nil {
			return e
		}
		if dirty != "" {
			return fmt.Errorf("workspace dirty: %s", dir)
		}
	}
	current, e := git(s.Root, "symbolic-ref", "--short", "HEAD")
	if e != nil || current != t.Target {
		return errors.New("project worktree must be on target branch")
	}
	if _, e = git(t.Workspace, "merge-base", "--is-ancestor", target, head); e != nil {
		return errors.New("target diverged: integrate in task worktree and resubmit")
	}
	return nil
}

func merged(s *State, t *Task) bool {
	if t.MergeIntent == nil {
		return false
	}
	// Exact equality is intentionally conservative; external branch changes need
	// operator investigation rather than silently blessing a different result.
	target, e := git(s.Root, "rev-parse", "refs/heads/"+t.Target)
	return e == nil && target == t.MergeIntent.SHA
}
func (st *Store) finishMerge(id string) error {
	var result *Task
	e := st.update(func(s *State) error {
		t, e := s.task(id)
		if e != nil {
			return e
		}
		if t.State == TaskPhaseDone {
			result = t
			return nil
		}
		if t.State != TaskPhaseMerging || !merged(s, t) {
			return errors.New("merge not confirmed; intent retained")
		}
		t.MergeCommit = t.MergeIntent.SHA
		t.State = TaskPhaseDone
		t.Updated = now()
		t.MergeIntent = nil
		s.event("master", "merged", id+" "+t.MergeCommit)
		result = t
		return nil
	})
	if e != nil {
		return e
	}
	return jsonOut(result)
}
func (st *Store) reconcile() error {
	unlock, e := filelock.Acquire(st.Dir, "merge", true)
	if e != nil {
		return nil
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	for _, t := range s.Tasks {
		if t.State == TaskPhaseMerging && merged(s, t) {
			if e = st.finishMerge(t.ID); e != nil {
				return e
			}
		}
	}
	return nil
}

// Clear an unexecuted intent without touching any files or resetting branches.
func abortMerge(st *Store, actor, id string) error {
	if actor != "master" {
		return fmt.Errorf("only master aborts merge intents: %w", ErrMasterRequired)
	}
	unlock, e := filelock.Acquire(st.Dir, "merge", false)
	if e != nil {
		return e
	}
	defer unlock()
	return st.update(func(s *State) error {
		t, e := s.task(id)
		if e != nil {
			return e
		}
		if t.State != TaskPhaseMerging || t.MergeIntent == nil {
			return errors.New("no pending merge intent")
		}
		target, e := git(s.Root, "rev-parse", "refs/heads/"+t.Target)
		if e != nil {
			return e
		}
		if target != t.MergeIntent.TargetSHA {
			return errors.New("target changed; reconcile or investigate before aborting")
		}
		t.MergeIntent = nil
		t.Approval = nil
		t.State = TaskPhaseInReview
		s.event(actor, "merge_aborted", id)
		return nil
	})
}
