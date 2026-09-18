package squad

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/filelock"
)

func (st *Store) prepareWorkspace(id string) error {
	unlock, e := filelock.Acquire(st.Dir, "workspace-"+id, false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	t, e := s.task(id)
	if e != nil {
		return e
	}
	if t.State != TaskPhasePreparing {
		return nil
	}
	if _, e = os.Stat(filepath.Join(t.Workspace, ".git")); os.IsNotExist(e) {
		if _, e = git(s.Root, "rev-parse", "--verify", "refs/heads/"+t.Branch); e == nil {
			head, e := git(s.Root, "rev-parse", "refs/heads/"+t.Branch)
			if e != nil || head != t.Base {
				return fmt.Errorf("workspace branch differs from recorded base; inspect %s", t.Branch)
			}
			_, e = git(s.Root, "worktree", "add", t.Workspace, t.Branch)
			if e != nil {
				return e
			}
		} else {
			if _, e = git(s.Root, "worktree", "add", "-b", t.Branch, t.Workspace, t.Base); e != nil {
				return e
			}
		}
	}
	branch, e := git(t.Workspace, "symbolic-ref", "--short", "HEAD")
	if e != nil || branch != t.Branch {
		return fmt.Errorf("workspace does not match durable intent")
	}
	common, e := git(t.Workspace, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return e
	}
	expected, e := git(s.Root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil || common != expected {
		return fmt.Errorf("workspace belongs to another repository")
	}
	return st.update(func(s *State) error {
		t := s.Tasks[id]
		if t.State != TaskPhasePreparing {
			return nil
		}
		t.State = TaskPhaseReady
		t.Updated = now()
		if t.Dispatch == DispatchModeOpen {
			for id, m := range s.Members {
				if id != "master" && m.State != MemberStateRemoved {
					s.message("master", id, t.ID, "Task available: "+t.ID+" "+t.Title+". Read board and claim if suitable.", "")
				}
			}
		}
		s.event("master", "workspace_ready", id)
		return nil
	})
}
