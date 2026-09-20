package squad

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/process"
)

type removalPlan struct {
	Team              string   `json:"team"`
	Directory         string   `json:"directory"`
	Worktrees         []string `json:"worktrees"`
	PreservedBranches []string `json:"preserved_branches"`
	IgnoredFiles      []string `json:"ignored_files"`
	DiscardIgnored    bool     `json:"discard_ignored"`
	DryRun            bool     `json:"dry_run"`
	Removed           bool     `json:"removed"`
}

// Removal is an explicit outside-terminal operation. It never stops a team or
// switches the identity of an agent session, and requires a named saved team.
func removeSavedTeam(o options) error {
	if os.Getenv("CSQUAD_STATE_DIR") != "" || os.Getenv("CSQUAD_MEMBER_ID") != "" || o["member"] != "" && o["member"] != "master" {
		return errors.New("remove a saved team from a terminal outside the team")
	}
	if o["name"] == "" {
		return errors.New("team remove requires a team name")
	}
	dir, err := namedTeam(o["name"])
	if err != nil {
		return err
	}
	plan, err := removeTeamWithIgnored(dir, o["dry-run"] == "true", o["discard-ignored"] == "true")
	if err != nil {
		return err
	}
	return jsonOut(plan)
}

func removalStore(dir string) (*Store, error) {
	// Unlike openStore, a typo or dry run must not create or initialize a ledger.
	if _, err := os.Stat(filepath.Join(dir, "state.db")); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, "state.db"), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{Dir: dir, DB: db}, nil
}

func removeTeamDirectory(dir string, dryRun bool) (*removalPlan, error) {
	return removeTeamWithIgnored(dir, dryRun, false)
}

func removeTeamWithIgnored(dir string, dryRun, discardIgnored bool) (*removalPlan, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if real != dir || filepath.Base(filepath.Dir(dir)) != "teams" {
		return nil, errors.New("refusing removal outside a real saved-team directory")
	}
	st, err := removalStore(dir)
	if err != nil {
		return nil, err
	}
	defer st.DB.Close()
	// Match resume/start locking, and refuse a runtime still using this ledger.
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	if !dryRun {
		for _, name := range []string{"team-lifecycle", "runtime", "runtime-start", "merge"} {
			release, e := filelock.Acquire(dir, name, true)
			if e != nil {
				return nil, fmt.Errorf("team is busy (%s): %w", name, e)
			}
			releases = append(releases, release)
		}
	}
	s, err := st.read()
	if err != nil {
		return nil, err
	}
	if s.ID != filepath.Base(dir) {
		return nil, errors.New("team name does not match saved directory")
	}
	if s.Active || s.Phase == TeamPhaseStarting || s.Phase == TeamPhaseStopping || s.Phase == TeamPhaseCleanupFailed {
		return nil, errors.New("team must be fully stopped before removal; use csquad stop")
	}
	if err = removalProcessesStopped(s); err != nil {
		return nil, err
	}
	plan, err := planTeamRemoval(st, s)
	if err != nil {
		return nil, err
	}
	plan.DryRun = dryRun
	plan.DiscardIgnored = discardIgnored
	if dryRun {
		return plan, nil
	}
	if len(plan.IgnoredFiles) > 0 && !discardIgnored {
		return nil, errors.New("worktrees contain ignored files; inspect team remove NAME --dry-run and explicitly use --discard-ignored to delete them")
	}
	// Validate the whole inventory before removing any worktree. Git performs
	// its own final dirty check; never use --force or delete branches here.
	for _, path := range plan.Worktrees {
		if _, err = git(s.Root, "worktree", "remove", path); err != nil {
			return nil, fmt.Errorf("remove worktree %s (team ledger retained): %w", path, err)
		}
	}
	if err = st.DB.Close(); err != nil {
		return nil, err
	}
	if err = os.RemoveAll(dir); err != nil {
		return nil, err
	}
	// Do not replace a current-team pointer with an arbitrary old team.
	pointer := filepath.Join(filepath.Dir(filepath.Dir(dir)), "last-team")
	if b, e := os.ReadFile(pointer); e == nil && cleanPath(strings.TrimSpace(string(b))) == dir {
		if err = os.Remove(pointer); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	plan.Removed = true
	return plan, nil
}

func removalProcessesStopped(s *State) error {
	all, err := process.Snapshot()
	if err != nil {
		return err
	}
	for _, m := range s.Members {
		for _, p := range m.Processes {
			if process.Alive(p, all) {
				return fmt.Errorf("member %s still has a live process", m.ID)
			}
		}
		for _, pid := range []int{m.RunnerPID, m.EnginePID} {
			if p, ok := all[pid]; ok && !strings.HasPrefix(p.Stat, "Z") && (m.ProcessStart == "" || m.ProcessStart == p.Start) {
				return fmt.Errorf("member %s still has a live process", m.ID)
			}
		}
	}
	// No socket means there cannot be an owned tmux server to inspect. If a
	// socket still exists, fail closed on inspection failure rather than guessing.
	if s.Socket != "" {
		if _, err = os.Stat(s.Socket); err == nil {
			out, e := tm(s, "list-sessions", "-F", "#{session_name}")
			if e != nil {
				return fmt.Errorf("cannot verify remaining tmux sessions: %w", e)
			}
			for _, name := range strings.Split(out, "\n") {
				if name == runtimeName(s) {
					return errors.New("team runtime session still exists")
				}
				for _, m := range s.Members {
					if name == m.Session {
						return fmt.Errorf("member session %s still exists", name)
					}
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func planTeamRemoval(st *Store, s *State) (*removalPlan, error) {
	plan := &removalPlan{Team: s.ID, Directory: st.Dir, Worktrees: []string{}, PreservedBranches: []string{}, IgnoredFiles: []string{}}
	base := filepath.Join(st.Dir, "worktrees")
	wanted := map[string]*Task{}
	for _, t := range s.Tasks {
		if t.MergeIntent != nil {
			return nil, fmt.Errorf("task %s has a pending merge intent", t.ID)
		}
		if t.Workspace == "" {
			continue
		}
		path := cleanPath(t.Workspace)
		if !filepath.IsAbs(path) || filepath.Dir(path) != base {
			return nil, fmt.Errorf("task %s workspace is outside team worktrees", t.ID)
		}
		if _, ok := wanted[path]; ok {
			return nil, fmt.Errorf("duplicate task workspace %s", path)
		}
		wanted[path] = t
	}
	if err := inspectRemovalPayload(st.Dir, s, wanted); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		path := filepath.Join(base, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || wanted[path] == nil {
			return nil, fmt.Errorf("unrecognized worktree entry %s; preserve it before removing team", path)
		}
	}
	out, err := git(s.Root, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		// Teams without code tasks may have been created outside Git, or
		// outlive their original directory. No Git work can be removed there.
		if len(wanted) == 0 && len(entries) == 0 {
			return plan, nil
		}
		return nil, err
	}
	registered := map[string]bool{}
	for _, field := range strings.Split(out, "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			path := strings.TrimPrefix(field, "worktree ")
			registered[path] = true
			if strings.HasPrefix(path, st.Dir+string(filepath.Separator)) && wanted[path] == nil {
				return nil, fmt.Errorf("unrecognized registered worktree %s", path)
			}
		}
	}
	for path, t := range wanted {
		if _, err = os.Lstat(path); os.IsNotExist(err) && !registered[path] {
			continue
		} // safely retry partial removal
		if err != nil {
			return nil, err
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil || resolved != path {
			return nil, fmt.Errorf("workspace path is not a real directory: %s", path)
		}
		if !registered[path] {
			return nil, fmt.Errorf("workspace is not registered: %s", path)
		}
		own, e := repoIdentity(path)
		common, ce := repoIdentity(s.Root)
		if e != nil || ce != nil || !sameDir(own, common) {
			return nil, fmt.Errorf("workspace repository changed: %s", path)
		}
		dirty, e := git(path, "status", "--porcelain", "--untracked-files=all")
		if e != nil {
			return nil, e
		}
		if dirty != "" {
			return nil, fmt.Errorf("workspace has uncommitted files: %s", path)
		}
		ignored, e := git(path, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
		if e != nil {
			return nil, e
		}
		for _, name := range strings.Split(ignored, "\x00") {
			if name != "" {
				plan.IgnoredFiles = append(plan.IgnoredFiles, filepath.Join(path, name))
			}
		}
		if t.Target == "" {
			return nil, fmt.Errorf("task %s has no target branch", t.ID)
		}
		if _, e = git(path, "merge-base", "--is-ancestor", "HEAD", "refs/heads/"+t.Target); e != nil {
			return nil, fmt.Errorf("workspace has commits not merged into %s: %s", t.Target, path)
		}
		branch, e := git(path, "symbolic-ref", "--quiet", "--short", "HEAD")
		if e == nil {
			plan.PreservedBranches = append(plan.PreservedBranches, branch)
		}
		plan.Worktrees = append(plan.Worktrees, path)
	}
	sort.Strings(plan.Worktrees)
	sort.Strings(plan.PreservedBranches)
	sort.Strings(plan.IgnoredFiles)
	return plan, nil
}

// A saved-team directory is not an arbitrary trash folder. Only generated
// metadata and explicitly validated task worktrees belong to this operation.
// Walk the entire tree, including ignored directories, to detect foreign/nested
// repositories before deleting anything. Never follow a symlink.
func inspectRemovalPayload(dir string, s *State, worktrees map[string]*Task) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		inWorktree := len(parts) >= 2 && parts[0] == "worktrees" && worktrees[filepath.Join(dir, parts[0], parts[1])] != nil
		if entry.Name() == ".git" {
			if !inWorktree || len(parts) != 3 || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("nested or foreign repository at %s; preserve it before removing team", path)
			}
		}
		if entry.IsDir() {
			if _, e := os.Stat(filepath.Join(path, "HEAD")); e == nil {
				if objects, e := os.Stat(filepath.Join(path, "objects")); e == nil && objects.IsDir() {
					return fmt.Errorf("possible bare repository at %s; preserve it before removing team", path)
				}
			}
		}
		if inWorktree {
			return nil
		}
		allowed := false
		switch parts[0] {
		case "state.db", "state.db-wal", "state.db-shm":
			allowed = len(parts) == 1 && entry.Type().IsRegular()
		case "worktrees":
			allowed = len(parts) == 1 && entry.IsDir()
		case "locks":
			allowed = len(parts) == 1 && entry.IsDir()
			if len(parts) == 2 && entry.Type().IsRegular() {
				name := strings.TrimSuffix(parts[1], ".lock")
				if name != parts[1] {
					allowed = contains([]string{"team-lifecycle", "runtime", "runtime-start", "merge", "panels", "navigation"}, name) || s.Members[strings.TrimPrefix(name, "member-")] != nil && strings.HasPrefix(name, "member-") || s.Tasks[strings.TrimPrefix(name, "workspace-")] != nil && strings.HasPrefix(name, "workspace-")
				}
			}
		case "handoffs":
			allowed = len(parts) == 1 && entry.IsDir()
			if len(parts) == 2 && entry.Type().IsRegular() && strings.HasSuffix(parts[1], ".json") {
				name := strings.TrimSuffix(parts[1], ".json")
				at := strings.LastIndex(name, "-")
				if at >= 0 {
					_, e := strconv.ParseUint(name[at+1:], 10, 64)
					allowed = e == nil && (name[:at] == "team" || s.Members[name[:at]] != nil)
				}
			}
		case "runtime":
			allowed = len(parts) == 1 && entry.IsDir()
			if len(parts) >= 2 && s.Members[parts[1]] != nil {
				allowed = len(parts) == 2 && entry.IsDir()
				if len(parts) == 3 && (parts[2] == "prompt.txt" || parts[2] == "claude.json") {
					allowed = entry.Type().IsRegular()
				}
				if len(parts) >= 3 {
					_, e := strconv.ParseUint(parts[2], 10, 64)
					if e == nil {
						allowed = len(parts) == 3 && entry.IsDir() || len(parts) == 4 && parts[3] == "bin" && entry.IsDir() || len(parts) == 5 && parts[3] == "bin" && parts[4] == "csquad" && entry.Type()&os.ModeSymlink != 0
					}
				}
			}
		}
		if !allowed {
			return fmt.Errorf("unrecognized team data %s; preserve it before removing team", path)
		}
		return nil
	})
}
