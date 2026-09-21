package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
)

type workspaceCleanup struct {
	Task      string `json:"task"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	DryRun    bool   `json:"dry_run"`
}

// Cleanup is an explicit master action, never a merge side effect. Keep task
// history and branch refs; only the redundant checkout is removed.
func cleanTaskWorkspace(st *Store, actor, id string, dryRun bool) (*workspaceCleanup, error) {
	if actor != "master" {
		return nil, fmt.Errorf("only master may clean worktrees: %w", ErrMasterRequired)
	}
	var unlocks []func()
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}()
	for _, name := range []string{"team-lifecycle", "merge", "workspace-" + id} {
		unlock, err := filelock.Acquire(st.Dir, name, true)
		if err != nil {
			return nil, fmt.Errorf("cleanup busy (%s): %w", name, err)
		}
		unlocks = append(unlocks, unlock)
	}
	var plan *workspaceCleanup
	// Keep member/task mutations serialized through the final Git operation.
	err := st.update(func(s *State) error {
		t, err := s.task(id)
		if err != nil {
			return err
		}
		plan = &workspaceCleanup{Task: id, Workspace: t.Workspace, Status: "retained", DryRun: dryRun}
		reason, absent := workspaceCleanupReason(st, s, t)
		if reason != "" {
			plan.Reason = reason
			return nil
		}
		if absent {
			plan.Status = "already_removed"
			return nil
		}
		plan.Status = "eligible"
		if dryRun {
			return nil
		}
		// Git performs a final dirty/locked-worktree check. Never use --force.
		if _, err = git(s.Root, "worktree", "remove", t.Workspace); err != nil {
			return err
		}
		plan.Status = "removed"
		s.event(actor, "workspace_removed", id+" "+t.Workspace)
		return nil
	})
	return plan, err
}

func workspaceCleanupReason(st *Store, s *State, t *Task) (string, bool) {
	if t.State != TaskPhaseDone || t.MergeCommit == "" || t.MergeIntent != nil || t.ExternalClosure != nil {
		return "task must have completed a confirmed merge", false
	}
	path := filepath.Clean(t.Workspace)
	if !filepath.IsAbs(t.Workspace) || path != t.Workspace || path != filepath.Join(st.Dir, "worktrees", t.ID) {
		return "workspace is outside the recorded task directory", false
	}
	if t.Target == "" || t.Branch == "" {
		return "task branch or target is missing", false
	}
	for _, other := range s.Tasks {
		if other.ID != t.ID && other.Workspace == path {
			return "workspace is shared by another task", false
		}
	}
	for _, m := range s.Members {
		if m.State == MemberStateRemoved {
			continue
		}
		cwd := m.Cwd
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
		if pathContains(path, cwd) {
			return "member " + m.ID + " still uses this working directory; move the member first", false
		}
	}
	if cwd, err := os.Getwd(); err != nil {
		return "cannot verify caller working directory", false
	} else if pathContains(path, cwd) {
		return "caller is inside this worktree", false
	}
	// Check both the durable merged commit and the current checkout; a branch
	// may have gained local commits since delivery.
	if _, err := git(s.Root, "merge-base", "--is-ancestor", t.MergeCommit, "refs/heads/"+t.Target); err != nil {
		return "recorded merge is not reachable from the target branch", false
	}
	registered, err := git(s.Root, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "cannot inspect registered worktrees", false
	}
	found := false
	current := false
	for _, field := range strings.Split(registered, "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			current = field == "worktree "+path
			found = found || current
		}
		if current && (field == "locked" || strings.HasPrefix(field, "locked ")) {
			return "worktree is locked", false
		}
	}
	_, err = os.Lstat(path)
	if os.IsNotExist(err) && !found {
		return "", true
	}
	if err != nil {
		return "workspace is missing or cannot be inspected; inspect Git registration", false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return "workspace path contains a symbolic link", false
	}
	if !found {
		return "workspace is not registered in the team repository", false
	}
	own, e := repoIdentity(path)
	common, ce := repoIdentity(s.Root)
	if e != nil || ce != nil || !sameDir(own, common) {
		return "workspace repository identity changed", false
	}
	branch, e := git(path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if e != nil || branch != t.Branch {
		return "workspace branch changed", false
	}
	if _, e = git(path, "merge-base", "--is-ancestor", "HEAD", "refs/heads/"+t.Target); e != nil {
		return "workspace contains commits not merged into the target", false
	}
	dirty, e := git(path, "status", "--porcelain", "--untracked-files=all", "--ignored")
	if e != nil {
		return "cannot inspect workspace files", false
	}
	if dirty != "" {
		return "workspace contains modified, untracked, or ignored files", false
	}
	return "", false
}

func pathContains(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
