package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupFixture(t *testing.T) (*Store, string) {
	t.Helper()
	st, path := savedRemovalTeam(t, true)
	must(t, os.WriteFile(filepath.Join(path, "tracked"), []byte("original"), 0600))
	_, err := git(path, "add", "tracked")
	must(t, err)
	_, err = git(path, "commit", "-m", "tracked fixture")
	must(t, err)
	s, err := st.read()
	must(t, err)
	_, err = git(s.Root, "merge", "--ff-only", s.Tasks["T1"].Branch)
	must(t, err)
	must(t, st.update(func(s *State) error {
		s.Members["master"] = &Member{ID: "master", Cwd: s.Root, State: MemberStateStopped}
		return nil
	}))
	sha, err := git(path, "rev-parse", "HEAD")
	must(t, err)
	must(t, st.update(func(s *State) error { s.Tasks["T1"].MergeCommit = sha; return nil }))
	return st, path
}

func TestCleanWorktreePreservesHistoryAndBranch(t *testing.T) {
	st, path := cleanupFixture(t)
	before := taskJSON(t, st, "T1")
	plan, err := cleanTaskWorkspace(st, "master", "T1", true)
	must(t, err)
	if plan.Status != "eligible" {
		t.Fatalf("preview: %+v", plan)
	}
	_, err = os.Stat(path)
	must(t, err)
	plan, err = cleanTaskWorkspace(st, "master", "T1", false)
	must(t, err)
	if plan.Status != "removed" {
		t.Fatalf("cleanup: %+v", plan)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkout retained: %v", err)
	}
	if taskJSON(t, st, "T1") != before {
		t.Fatal("cleanup changed delivery evidence")
	}
	s, err := st.read()
	must(t, err)
	_, err = git(s.Root, "rev-parse", "refs/heads/"+s.Tasks["T1"].Branch)
	must(t, err)
	out, err := git(s.Root, "worktree", "list", "--porcelain")
	must(t, err)
	if strings.Contains(out, path) {
		t.Fatal("registration retained")
	}
	plan, err = cleanTaskWorkspace(st, "master", "T1", false)
	must(t, err)
	if plan.Status != "already_removed" {
		t.Fatalf("not idempotent: %+v", plan)
	}
}

func TestCleanWorktreeRetainsUnsafeCheckouts(t *testing.T) {
	for _, mode := range []string{"unfinished", "no-merge", "member", "member-child", "ignored", "untracked", "dirty", "unmerged", "symlink", "branch", "locked"} {
		t.Run(mode, func(t *testing.T) {
			st, path := cleanupFixture(t)
			switch mode {
			case "unfinished":
				must(t, st.update(func(s *State) error { s.Tasks["T1"].State = TaskPhaseInReview; return nil }))
			case "no-merge":
				must(t, st.update(func(s *State) error { s.Tasks["T1"].MergeCommit = ""; return nil }))
			case "member", "member-child":
				cwd := path
				if mode == "member-child" {
					cwd = filepath.Join(path, "child")
				}
				must(t, st.update(func(s *State) error {
					s.Members["worker"] = &Member{ID: "worker", Cwd: cwd, State: MemberStateStopped}
					return nil
				}))
			case "ignored":
				must(t, os.WriteFile(filepath.Join(path, ".gitignore"), []byte("cache\n"), 0600))
				_, err := git(path, "add", ".gitignore")
				must(t, err)
				_, err = git(path, "commit", "-m", "ignore cache")
				must(t, err)
				s, err := st.read()
				must(t, err)
				_, err = git(s.Root, "merge", "--ff-only", s.Tasks["T1"].Branch)
				must(t, err)
				must(t, os.WriteFile(filepath.Join(path, "cache"), []byte("keep"), 0600))
			case "untracked":
				must(t, os.WriteFile(filepath.Join(path, "new-file"), []byte("keep"), 0600))
			case "dirty":
				files, err := git(path, "ls-files")
				must(t, err)
				must(t, os.WriteFile(filepath.Join(path, strings.Split(files, "\n")[0]), []byte("changed"), 0600))
			case "unmerged":
				_, err := git(path, "commit", "--allow-empty", "-m", "unmerged")
				must(t, err)
			case "symlink":
				must(t, os.Rename(path, path+"-real"))
				must(t, os.Symlink(path+"-real", path))
			case "branch":
				_, err := git(path, "checkout", "-b", "unexpected")
				must(t, err)
			case "locked":
				_, err := git(path, "worktree", "lock", path)
				must(t, err)
			}
			plan, err := cleanTaskWorkspace(st, "master", "T1", false)
			if err == nil && (plan.Status != "retained" || plan.Reason == "") {
				t.Fatalf("unsafe removal: %+v", plan)
			}
			if _, err = os.Lstat(path); err != nil {
				t.Fatalf("lost checkout: %v", err)
			}
		})
	}
}

func TestCleanWorktreeAuthorizationAndStoppedExecute(t *testing.T) {
	st, path := cleanupFixture(t)
	if _, err := cleanTaskWorkspace(st, "worker", "T1", false); err == nil {
		t.Fatal("worker removed checkout")
	}
	for _, key := range []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION"} {
		t.Setenv(key, "")
	}
	must(t, Execute([]string{"task", "clean-worktree", "T1"}, options{"team": st.Dir, "dry-run": "true"}, nil))
	_, err := os.Stat(path)
	must(t, err)
	s, err := st.read()
	must(t, err)
	if s.Active {
		t.Fatal("preview started runtime")
	}
}
