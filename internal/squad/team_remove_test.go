package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/process"
)

func savedRemovalTeam(t *testing.T, withWorktree bool) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	initRepo(t, root)
	dir := filepath.Join(root, ".csquad", "teams", "old")
	st, e := openStore(dir)
	must(t, e)
	t.Cleanup(func() { st.DB.Close() })
	workspace := filepath.Join(dir, "worktrees", "T1")
	if withWorktree {
		_, e = git(root, "worktree", "add", "-b", "csquad/old/T1", workspace)
		must(t, e)
	}
	must(t, st.update(func(s *State) error {
		*s = State{Version: 1, ID: "old", Root: root, Phase: TeamPhaseStopped, Members: map[string]*Member{}, Tasks: map[string]*Task{}, Questions: map[string]*Question{}}
		if withWorktree {
			s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseDone, Workspace: workspace, Branch: "csquad/old/T1", Target: "main"}
		}
		return nil
	}))
	return st, workspace
}

func TestSavedTeamRemovalDryRunAndRemove(t *testing.T) {
	st, workspace := savedRemovalTeam(t, true)
	s, e := st.read()
	must(t, e)
	pointer := filepath.Join(filepath.Dir(filepath.Dir(st.Dir)), "last-team")
	must(t, os.WriteFile(pointer, []byte(st.Dir), 0600))
	before, e := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, e)
	plan, e := removeTeamDirectory(st.Dir, true)
	must(t, e)
	if !plan.DryRun || plan.Removed || len(plan.Worktrees) != 1 || len(plan.PreservedBranches) != 1 {
		t.Fatalf("unexpected preview: %+v", plan)
	}
	after, e := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, e)
	if string(before) != string(after) {
		t.Fatal("dry-run changed ledger")
	}
	if _, e = os.Stat(workspace); e != nil {
		t.Fatal("dry-run removed workspace", e)
	}
	plan, e = removeTeamDirectory(st.Dir, false)
	must(t, e)
	if !plan.Removed {
		t.Fatal("removal not reported")
	}
	if _, e = os.Stat(st.Dir); !os.IsNotExist(e) {
		t.Fatalf("team directory survived: %v", e)
	}
	if _, e = os.Stat(pointer); !os.IsNotExist(e) {
		t.Fatalf("stale last-team pointer survived: %v", e)
	}
	out, e := git(s.Root, "worktree", "list", "--porcelain")
	must(t, e)
	if strings.Contains(out, workspace) {
		t.Fatal("Git registration survived")
	}
	_, e = git(s.Root, "rev-parse", "--verify", "refs/heads/csquad/old/T1")
	must(t, e)
}

func TestSavedTeamRemovalRefusesUnsafeWork(t *testing.T) {
	for _, mode := range []string{"active", "dirty", "untracked", "unmerged", "external", "unknown", "symlink", "merge-intent", "process", "lock"} {
		t.Run(mode, func(t *testing.T) {
			st, path := savedRemovalTeam(t, true)
			s, e := st.read()
			must(t, e)
			switch mode {
			case "active":
				must(t, st.update(func(s *State) error { s.Active = true; return nil }))
			case "dirty":
				must(t, os.WriteFile(filepath.Join(path, "tracked"), []byte("new"), 0600))
				_, e = git(path, "add", "tracked")
				must(t, e)
			case "untracked":
				must(t, os.WriteFile(filepath.Join(path, "untracked"), []byte("new"), 0600))
			case "unmerged":
				commitFile(t, path, "work", "valuable")
			case "external":
				must(t, st.update(func(s *State) error { s.Tasks["T1"].Workspace = t.TempDir(); return nil }))
			case "unknown":
				must(t, os.Mkdir(filepath.Join(st.Dir, "worktrees", "unknown"), 0700))
			case "symlink":
				must(t, os.Symlink(t.TempDir(), filepath.Join(st.Dir, "worktrees", "link")))
			case "merge-intent":
				must(t, st.update(func(s *State) error { s.Tasks["T1"].MergeIntent = &Approval{}; return nil }))
			case "process":
				all, e := process.Snapshot()
				must(t, e)
				p := all[os.Getpid()]
				must(t, st.update(func(s *State) error {
					s.Members["master"] = &Member{ID: "master", Processes: []process.Identity{p}}
					return nil
				}))
			case "lock":
				unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
				must(t, e)
				defer unlock()
			}
			if _, e = removeTeamDirectory(st.Dir, false); e == nil {
				t.Fatal("unsafe removal succeeded")
			}
			if _, e = os.Stat(filepath.Join(st.Dir, "state.db")); e != nil {
				t.Fatal("ledger removed on refusal", e)
			}
			if _, e = os.Stat(path); e != nil {
				t.Fatal("workspace removed on refusal", e)
			}
			out, e := git(s.Root, "worktree", "list", "--porcelain")
			must(t, e)
			if !strings.Contains(out, path) {
				t.Fatal("registration removed on refusal")
			}
		})
	}
}

func TestSavedTeamRemovalEmptyAndOtherPointer(t *testing.T) {
	st, _ := savedRemovalTeam(t, false)
	pointer := filepath.Join(filepath.Dir(filepath.Dir(st.Dir)), "last-team")
	must(t, os.WriteFile(pointer, []byte("/another/team"), 0600))
	_, e := removeTeamDirectory(st.Dir, false)
	must(t, e)
	b, e := os.ReadFile(pointer)
	must(t, e)
	if string(b) != "/another/team" {
		t.Fatal("other team's pointer changed")
	}
}

func TestSavedTeamRemovalRequiresOutsideNamedTeam(t *testing.T) {
	if e := removeSavedTeam(options{}); e == nil {
		t.Fatal("unnamed removal accepted")
	}
	t.Setenv("CSQUAD_STATE_DIR", "/bound/team")
	if e := removeSavedTeam(options{"name": "old"}); e == nil || !strings.Contains(e.Error(), "outside") {
		t.Fatalf("bound session removal: %v", e)
	}
}

func TestSavedTeamRemovalRefusesUnrecordedRegisteredWorktree(t *testing.T) {
	st, _ := savedRemovalTeam(t, false)
	s, e := st.read()
	must(t, e)
	path := filepath.Join(st.Dir, "unexpected")
	_, e = git(s.Root, "worktree", "add", "-b", "unrecorded", path)
	must(t, e)
	if _, e = removeTeamDirectory(st.Dir, false); e == nil || !strings.Contains(e.Error(), "unrecognized") {
		t.Fatalf("unrecorded registered worktree removal: %v", e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal(e)
	}
}

func TestSavedTeamRemovalPreservesIgnoredSourceUnlessExplicit(t *testing.T) {
	st, path := savedRemovalTeam(t, true)
	commitFile(t, path, ".gitignore", "secret-source.txt\n")
	s, e := st.read()
	must(t, e)
	_, e = git(s.Root, "merge", "--ff-only", "csquad/old/T1")
	must(t, e)
	source := filepath.Join(path, "secret-source.txt")
	must(t, os.WriteFile(source, []byte("valuable ignored source"), 0600))
	plan, e := removeTeamDirectory(st.Dir, true)
	must(t, e)
	if len(plan.IgnoredFiles) != 1 || plan.IgnoredFiles[0] != source {
		t.Fatalf("ignored inventory: %+v", plan)
	}
	if _, e = removeTeamDirectory(st.Dir, false); e == nil || !strings.Contains(e.Error(), "ignored files") {
		t.Fatalf("ignored source removal: %v", e)
	}
	if _, e = os.Stat(source); e != nil {
		t.Fatal("ignored source lost", e)
	}
	plan, e = removeTeamWithIgnored(st.Dir, false, true)
	must(t, e)
	if !plan.Removed || !plan.DiscardIgnored {
		t.Fatalf("explicit removal: %+v", plan)
	}
}

func TestSavedTeamRemovalPreservesForeignAndNestedRepositories(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign", true: "nested ignored"}[nested], func(t *testing.T) {
			st, path := savedRemovalTeam(t, nested)
			foreign := t.TempDir()
			initRepo(t, foreign)
			location := filepath.Join(st.Dir, "foreign-work")
			if nested {
				commitFile(t, path, ".gitignore", "nested/\n")
				s, e := st.read()
				must(t, e)
				_, e = git(s.Root, "merge", "--ff-only", "csquad/old/T1")
				must(t, e)
				location = filepath.Join(path, "nested")
			}
			_, e := git(foreign, "worktree", "add", "-b", "foreign-work", location)
			must(t, e)
			source := filepath.Join(location, "uncommitted-source.go")
			must(t, os.WriteFile(source, []byte("valuable source"), 0600))
			if _, e = removeTeamWithIgnored(st.Dir, false, true); e == nil {
				t.Fatal("foreign repository removal allowed")
			}
			if _, e = os.Stat(source); e != nil {
				t.Fatal("foreign source lost", e)
			}
		})
	}
}

func TestSavedTeamRemovalRefusesUnknownMetadata(t *testing.T) {
	for _, relative := range []string{"notes.md", "handoffs/notes.txt", "runtime/unknown/notes.txt", "locks/notes.txt"} {
		t.Run(relative, func(t *testing.T) {
			st, _ := savedRemovalTeam(t, false)
			path := filepath.Join(st.Dir, relative)
			must(t, os.MkdirAll(filepath.Dir(path), 0700))
			must(t, os.WriteFile(path, []byte("preserve"), 0600))
			if _, e := removeTeamWithIgnored(st.Dir, false, true); e == nil {
				t.Fatal("unknown payload removed")
			}
			if _, e := os.Stat(path); e != nil {
				t.Fatal(e)
			}
		})
	}
}
