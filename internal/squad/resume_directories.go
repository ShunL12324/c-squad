package squad

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
)

// Validate the whole roster before cleanup or starting even the first process.
func validateResumeDirectories(s *State) error {
	var failures []string
	for _, m := range s.Members {
		if m.State == MemberStateRemoved {
			continue
		}
		if _, err := memberDirectory("", m.Cwd); err != nil || m.Cwd == "" {
			failures = append(failures, fmt.Sprintf("member %s (engine %s), cwd %q: %v", m.ID, m.Engine, m.Cwd, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	sort.Strings(failures)
	return fmt.Errorf("cannot resume: invalid working directories:\n%s\nRestore the original directories/worktrees, or use csquad --team-name %s member set-cwd NAME --cwd /existing/directory, then resume", strings.Join(failures, "\n"), s.ID)
}

func setStoppedMemberDirectory(st *Store, actor, id, directory string) error {
	if actor != "master" {
		return ErrMasterRequired
	}
	if directory == "" {
		return errors.New("--cwd required")
	}
	cwd, err := memberDirectory("", directory)
	if err != nil {
		return err
	}
	unlock, err := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if err != nil {
		return err
	}
	defer unlock()
	return st.update(func(s *State) error {
		if s.Active {
			return errors.New("set-cwd requires a stopped team; use member restart NAME --cwd for a running team")
		}
		m, err := s.member(id)
		if err != nil {
			return err
		}
		if m.State == MemberStateRemoved {
			return errors.New("member is removed")
		}
		s.event(actor, "member_directory_changed", fmt.Sprintf("%s: %s -> %s", id, m.Cwd, cwd))
		m.Cwd = cwd
		return nil
	})
}
