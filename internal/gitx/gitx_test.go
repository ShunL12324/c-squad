package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, g Git, dir string, args ...string) {
	t.Helper()
	if _, err := g.Run(dir, args...); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) (Git, string) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g := Git{Env: append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, "none"), "GIT_CONFIG_NOSYSTEM=1")}
	r := filepath.Join(dir, "repo")
	os.MkdirAll(r, 0o755)
	run(t, g, r, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r, "a"), []byte("a"), 0o644)
	run(t, g, r, "add", ".")
	run(t, g, r, "commit", "-qm", "init")
	return g, r
}

func TestTaskRefsByNaming(t *testing.T) {
	g, r := repo(t)
	run(t, g, r, "worktree", "add", "-q", "-b", "worktree-t12-fix", filepath.Join(r, ".claude", "worktrees", "t12-fix"))
	run(t, g, r, "branch", "task/t12-extra")
	run(t, g, r, "branch", "task/t120-other") // T120, not T12
	run(t, g, r, "worktree", "add", "-q", "--detach", filepath.Join(r, "..", "t12-detached"))
	run(t, g, r, "worktree", "add", "-q", "-b", "mine", filepath.Join(r, "..", "t12-named"))
	refs, err := g.TaskRefs(r, 12, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(refs.Branches)
	if strings.Join(refs.Branches, ",") != "mine,task/t12-extra,worktree-t12-fix" {
		t.Errorf("branches %v", refs.Branches)
	}
	var paths []string
	for _, w := range refs.Worktrees {
		paths = append(paths, filepath.Base(w.Path))
	}
	sort.Strings(paths)
	if strings.Join(paths, ",") != "t12-detached,t12-fix,t12-named" {
		t.Errorf("worktrees %v", paths)
	}
	for _, w := range refs.Worktrees {
		if filepath.Base(w.Path) == "t12-detached" && (!w.Detached || w.Head == "") {
			t.Errorf("%+v", w)
		}
	}
}

// Refs named for a task but made before it belong to an older task with the
// same number: they are reported, never returned.
func TestTaskRefsOlderThanTheTaskAreIgnored(t *testing.T) {
	g, r := repo(t)
	day := time.Now().Add(-24 * time.Hour)
	old := Git{Env: append(append([]string{}, g.Env...), "GIT_COMMITTER_DATE="+day.Format(time.RFC3339))}
	run(t, old, r, "worktree", "add", "-q", "-b", "worktree-t7-old", filepath.Join(r, "..", "t7-old"))
	os.Chtimes(filepath.Join(r, "..", "t7-old", ".git"), day, day)
	run(t, old, r, "branch", "task/t7-older")
	run(t, old, r, "worktree", "add", "-q", "--detach", filepath.Join(r, "..", "t7-gone"))
	os.Chtimes(filepath.Join(r, "..", "t7-gone", ".git"), day, day)
	since := time.Now().Add(-time.Minute)
	run(t, g, r, "worktree", "add", "-q", "-b", "worktree-t7-new", filepath.Join(r, "..", "t7-new"))
	run(t, g, r, "branch", "task/t7-extra")
	refs, err := g.TaskRefs(r, 7, since)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(refs.Branches)
	sort.Strings(refs.Ignored)
	if strings.Join(refs.Branches, ",") != "task/t7-extra,worktree-t7-new" || len(refs.Worktrees) != 1 || filepath.Base(refs.Worktrees[0].Path) != "t7-new" {
		t.Errorf("kept %v %v", refs.Branches, refs.Worktrees)
	}
	want := []string{"branch task/t7-older", "branch worktree-t7-old", "worktree " + filepath.Join(filepath.Dir(r), "t7-gone"), "worktree " + filepath.Join(filepath.Dir(r), "t7-old")}
	if strings.Join(refs.Ignored, "|") != strings.Join(want, "|") {
		t.Errorf("ignored %q", refs.Ignored)
	}
	// Without a time every named ref counts.
	if all, _ := g.TaskRefs(r, 7, time.Time{}); len(all.Branches) != 4 || len(all.Ignored) != 0 {
		t.Errorf("all %+v", all)
	}
}

func TestAncestorDirtyAndRemoval(t *testing.T) {
	g, r := repo(t)
	wt := filepath.Join(r, "..", "t1-x")
	run(t, g, r, "worktree", "add", "-q", "-b", "task/t1-x", wt)
	run(t, g, r, "worktree", "lock", wt)
	os.WriteFile(filepath.Join(wt, "b"), []byte("b"), 0o644)
	if d, _ := g.Dirty(wt); !strings.Contains(d, "?? b") {
		t.Errorf("dirty %q", d)
	}
	run(t, g, wt, "add", ".")
	run(t, g, wt, "commit", "-qm", "b")
	if ok, err := g.IsAncestor(r, "task/t1-x", "main"); ok || err != nil {
		t.Errorf("not merged yet: %v %v", ok, err)
	}
	if _, err := g.IsAncestor(r, "nope", "main"); err == nil {
		t.Error("a missing ref is an error, not 'not merged'")
	}
	run(t, g, r, "merge", "-q", "--ff-only", "task/t1-x")
	if ok, _ := g.IsAncestor(r, "task/t1-x", "main"); !ok {
		t.Error("merged")
	}
	wts, _ := g.Worktrees(r)
	if len(wts) != 2 || !wts[1].Locked || wts[0].Branch != "main" {
		t.Fatalf("%+v", wts)
	}
	if err := g.RemoveWorktree(r, wts[1]); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteBranch(r, "task/t1-x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree left")
	}
}
