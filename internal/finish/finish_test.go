package finish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/claude"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func commit(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", file)
	run(t, dir, "commit", "-qm", "add "+file)
}

// repo returns a repository on main with one commit and a worktree on branch
// "task" under .claude/worktrees, like the ones Claude Code creates.
func repo(t *testing.T) (root, wt string) {
	t.Helper()
	root, _ = filepath.EvalSymlinks(t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	run(t, root, "init", "-q", "-b", "main")
	commit(t, root, "base")
	wt = filepath.Join(root, ".claude", "worktrees", "task")
	run(t, root, "worktree", "add", "-q", "-b", "task", wt)
	commit(t, wt, "feature")
	return root, wt
}

func removed(called *bool) Session {
	return Session{Live: true, Remove: func() error { *called = true; return nil }}
}

func exists(ref, root string) bool {
	_, err := git(root, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func TestMergedWorktreeIsRemoved(t *testing.T) {
	root, wt := repo(t)
	run(t, root, "merge", "-q", "task")
	// The session entered the worktree from a subdirectory and left again.
	locs := []claude.Location{{Cwd: root, Branch: "main"}, {Cwd: filepath.Join(wt, "sub"), Branch: "task"}, {Cwd: root, Branch: "main"}}
	p, err := Build(root, claude.Trail{Locations: locs}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Target != "main" || len(p.Worktrees) != 1 || p.Worktrees[0].Path != wt || strings.Join(p.Branches, ",") != "task" || len(p.Blockers) != 0 {
		t.Fatalf("plan = %+v", p)
	}
	run(t, root, "worktree", "lock", wt)
	var called bool
	if err := Run(p, removed(&called)); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("session not removed")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree still present: %v", err)
	}
	if exists("refs/heads/task", root) || !exists("refs/heads/main", root) {
		t.Fatal("wrong branches after finish")
	}
}

func TestUnmergedOrDirtyWorkBlocks(t *testing.T) {
	root, wt := repo(t)
	locs := []claude.Location{{Cwd: wt, Branch: "task"}}
	p, err := Build(root, claude.Trail{Locations: locs}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 1 || !strings.Contains(p.Blockers[0], "1 commit(s) not in main") {
		t.Fatalf("unmerged: %+v", p.Blockers)
	}
	var called bool
	if err := Run(p, removed(&called)); err == nil || called {
		t.Fatal("ran a blocked plan")
	}
	run(t, root, "merge", "-q", "task")
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ = Build(root, claude.Trail{Locations: locs}, "")
	if len(p.Blockers) != 1 || !strings.Contains(p.Blockers[0], "scratch.txt") {
		t.Fatalf("dirty: %+v", p.Blockers)
	}
}

func TestInPlaceWork(t *testing.T) {
	root, _ := repo(t)
	// Directly on the target: nothing to delete.
	p, err := Build(root, claude.Trail{Locations: []claude.Location{{Cwd: root, Branch: "main"}}}, "")
	if err != nil || len(p.Branches)+len(p.Worktrees)+len(p.Blockers) != 0 {
		t.Fatalf("on target: %+v %v", p, err)
	}
	// On its own branch in the main checkout, since merged.
	run(t, root, "switch", "-q", "-c", "inplace")
	commit(t, root, "inplace")
	run(t, root, "switch", "-q", "main")
	run(t, root, "merge", "-q", "inplace")
	p, _ = Build(root, claude.Trail{Locations: []claude.Location{{Cwd: root, Branch: "inplace"}}}, "")
	if strings.Join(p.Branches, ",") != "inplace" || len(p.Blockers) != 0 {
		t.Fatalf("in-place branch: %+v", p)
	}
	if err := Run(p, Session{}); err != nil {
		t.Fatal(err)
	}
	if exists("refs/heads/inplace", root) {
		t.Fatal("in-place branch kept")
	}
	// Created, committed and left within one command: only the created list
	// knows about it.
	run(t, root, "switch", "-q", "-c", "quick")
	commit(t, root, "quick")
	run(t, root, "switch", "-q", "main")
	run(t, root, "merge", "-q", "quick")
	p, _ = Build(root, claude.Trail{Locations: []claude.Location{{Cwd: root, Branch: "main"}}, Created: []string{"quick", "main", "nonexistent"}}, "")
	if strings.Join(p.Branches, ",") != "quick" || len(p.Blockers) != 0 {
		t.Fatalf("created branch: %+v", p)
	}
}

func TestGoneWorktreeBranchAndInto(t *testing.T) {
	root, wt := repo(t)
	run(t, root, "worktree", "remove", wt)
	run(t, root, "branch", "develop", "task")
	locs := []claude.Location{{Cwd: wt, Branch: "task"}}
	p, _ := Build(root, claude.Trail{Locations: locs}, "")
	if len(p.Blockers) != 1 {
		t.Fatalf("expected task unmerged into main: %+v", p)
	}
	p, err := Build(root, claude.Trail{Locations: locs}, "develop")
	if err != nil || len(p.Blockers) != 0 || strings.Join(p.Branches, ",") != "task" || len(p.Worktrees) != 0 {
		t.Fatalf("into develop: %+v %v", p, err)
	}
	if _, err := Build(root, claude.Trail{Locations: locs}, "missing"); err == nil {
		t.Fatal("accepted a missing target")
	}
}

func TestOutsideGit(t *testing.T) {
	dir := t.TempDir()
	p, err := Build(dir, claude.Trail{Locations: []claude.Location{{Cwd: dir}}}, "")
	if err != nil || p.Root != "" || len(p.Notes) != 1 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	var called bool
	if err := Run(p, removed(&called)); err != nil || !called {
		t.Fatalf("outside git: %v %v", called, err)
	}
}
