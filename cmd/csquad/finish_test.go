package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

var gitIdentity = []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_NOSYSTEM=1"}

func git(t *testing.T, h *testutil.Home, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = h.Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoWithTask makes a repository, launches task T1 in it and plays its
// worker: a worktree t1-fix on branch worktree-t1-fix with one commit; the
// session ends idle in that worktree.
func repoWithTask(t *testing.T) (*testutil.Home, string, string) {
	h := testutil.NewHome(t)
	h.ExtraEnv = gitIdentity
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(repo, 0o755)
	git(t, h, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "init")
	n := launchedRe.FindStringSubmatch(ok(t, csq(t, h, opts{stdin: "go", env: mine()}, "launch", "--title", "fix", "--cwd", repo)).stdout)[1]
	wt := filepath.Join(repo, ".claude", "worktrees", "t1-fix")
	git(t, h, repo, "worktree", "add", "-q", "-b", "worktree-t1-fix", wt)
	git(t, h, repo, "worktree", "lock", wt) // Claude Code locks the worktrees it creates
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	git(t, h, wt, "add", ".")
	git(t, h, wt, "commit", "-qm", "fix")
	setState(h, n, func(s *testutil.Session) { s.State, s.Status, s.Cwd = "working", "idle", wt })
	return h, repo, wt
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestFinishVerifiesThenRemovesGitFirstThenTheSession(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	// Not merged yet: refused, nothing removed.
	r := csq(t, h, opts{env: mine()}, "finish", "T1")
	if r.code != 1 || !strings.Contains(r.stderr, "branch worktree-t1-fix is not merged into main") || !strings.Contains(r.stderr, "nothing was removed") {
		t.Fatalf("%+v", r)
	}
	if !exists(wt) || len(h.Sessions()) != 1 {
		t.Fatal("something was removed")
	}
	git(t, h, repo, "merge", "-q", "--ff-only", "worktree-t1-fix")
	// Uncommitted changes: refused.
	os.WriteFile(filepath.Join(wt, "c.txt"), []byte("c\n"), 0o644)
	r = csq(t, h, opts{env: mine()}, "finish", "T1")
	if r.code != 1 || !strings.Contains(r.stderr, "has uncommitted changes") || !strings.Contains(r.stderr, "uncommitted changes: only untracked files: c.txt") {
		t.Fatalf("%+v", r)
	}
	os.Remove(filepath.Join(wt, "c.txt"))
	// Still working: refused.
	setState(h, "1", func(s *testutil.Session) { s.Status = "busy" })
	if r := csq(t, h, opts{env: mine()}, "finish", "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is working") {
		t.Fatalf("%+v", r)
	}
	setState(h, "1", func(s *testutil.Session) { s.Status = "idle" })
	h.ResetArgv()
	// The fake claude rm refuses while the session's worktree exists: the
	// worktree must go (with git) before the session.
	r = ok(t, csq(t, h, opts{env: mine("FAKE_RM_NEEDS_WORKTREE_GONE=1")}, "finish", "T1"))
	if !strings.Contains(r.stdout, "finished T1 · fix · merged into main: worktree-t1-fix · removed worktree "+wt+", branch worktree-t1-fix, removed session") {
		t.Errorf("%s", r.stdout)
	}
	if exists(wt) || strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") || len(h.Sessions()) != 0 {
		t.Error("not cleaned up")
	}
	if argv := h.LastArgv(); argv[0] != "rm" || testutil.Arg(argv, "--discard-unpushed") != "" {
		t.Errorf("claude rm without a token: %q", argv)
	}
	if exists(filepath.Join(h.Csquad, "settings", "T1.json")) {
		t.Error("the settings file (possibly a token) is left")
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; strings.Contains(out, "T1") {
		t.Errorf("a finished task still listed: %s", out)
	}
	if r := csq(t, h, opts{env: mine()}, "finish", "T1"); r.code != 1 || !strings.Contains(r.stderr, "already finished") {
		t.Errorf("%+v", r)
	}
}

// A branch named for T1 that is older than the task (csquad 1.x reused
// numbers) is neither checked nor removed; finish names it.
func TestFinishIgnoresOlderRefsOfTheSameNumber(t *testing.T) {
	h, repo, _ := repoWithTask(t)
	git(t, h, repo, "merge", "-q", "--ff-only", "worktree-t1-fix")
	cmd := exec.Command("git", "-C", repo, "branch", "task/t1-ancient", "worktree-t1-fix~1")
	cmd.Env = h.Env("GIT_COMMITTER_DATE=" + time.Now().Add(-72*time.Hour).Format(time.RFC3339))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	git(t, h, repo, "commit", "-q", "--allow-empty", "-m", "unrelated")
	r := ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if !strings.Contains(r.stdout, "ignored, older than the task (another task with its number made them): branch task/t1-ancient") {
		t.Errorf("%s", r.stdout)
	}
	if !strings.Contains(git(t, h, repo, "branch"), "task/t1-ancient") {
		t.Error("the older branch was removed")
	}
}

// claude rm keeps a session whose worktree holds commits it has not seen
// pushed, and prints a --discard-unpushed value: csquad uses it only when the
// session's own worktree was verified merged and removed.
func TestFinishDiscardTokenOnlyForAVerifiedWorktree(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	git(t, h, repo, "merge", "-q", "--ff-only", "worktree-t1-fix")
	h.ExtraEnv = append(h.ExtraEnv, "FAKE_RM_REFUSE=1")
	r := ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if argv := h.LastArgv(); testutil.Arg(argv, "--discard-unpushed") != "0123abc@wt-1" {
		t.Errorf("token not passed: %q\n%s", argv, r.stdout)
	}
	if exists(wt) || len(h.Sessions()) != 0 {
		t.Error("not cleaned up")
	}

	// A session whose cwd is not a verified worktree: the token is not used.
	h2 := testutil.NewHome(t)
	h2.ExtraEnv = append(gitIdentity, "FAKE_RM_REFUSE=1")
	ok(t, csq(t, h2, opts{stdin: "go", env: mine()}, "launch", "--title", "plain", "--cwd", h2.Root))
	setState(h2, "1", func(s *testutil.Session) { s.Status, s.Cwd = "idle", h2.Root })
	r = ok(t, csq(t, h2, opts{env: mine()}, "finish", "T1"))
	if !strings.Contains(r.stdout, "the session is left") || len(h2.Sessions()) != 1 {
		t.Errorf("%s", r.stdout)
	}
	for _, a := range h2.Argv() {
		if testutil.Arg(a, "--discard-unpushed") != "" {
			t.Errorf("token used for an unverified worktree: %q", a)
		}
	}
}

func TestFinishInPlaceTouchesNoGit(t *testing.T) {
	h := testutil.NewHome(t)
	h.ExtraEnv = gitIdentity
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(repo, 0o755)
	git(t, h, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "init")
	ok(t, csq(t, h, opts{stdin: "go", env: mine()}, "launch", "--title", "live", "--cwd", repo, "--isolation", "none"))
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed by the task\n"), 0o644)
	setState(h, "1", func(s *testutil.Session) { s.Status = "idle" })
	ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "changed by the task\n" || len(h.Sessions()) != 0 {
		t.Error("an in-place finish must remove the session only")
	}
}

// --discard on a cancelled in-place task never touches the user's checkout.
func TestDiscardAfterCancelInPlaceTouchesNoGit(t *testing.T) {
	h := testutil.NewHome(t)
	h.ExtraEnv = gitIdentity
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(repo, 0o755)
	git(t, h, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "init")
	ok(t, csq(t, h, opts{stdin: "go", env: mine()}, "launch", "--title", "live", "--cwd", repo, "--isolation", "none"))
	git(t, h, repo, "branch", "worktree-t1-live") // named for the task, but in the user's checkout
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed by the task\n"), 0o644)
	ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1"))
	ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1", "--discard"))
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "changed by the task\n" || !strings.Contains(git(t, h, repo, "branch"), "worktree-t1-live") {
		t.Error("--discard touched an in-place task's checkout")
	}
}

func TestCancelKeepsUnmergedWorkUnlessDiscard(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	setState(h, "1", func(s *testutil.Session) { s.Status = "busy" })
	r := ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1"))
	if !strings.Contains(r.stdout, "cancelled T1 · fix · stopped session") || !strings.Contains(r.stdout, "csquad cancel T1 --discard") {
		t.Errorf("%s", r.stdout)
	}
	if !exists(wt) || !strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") {
		t.Error("unmerged work removed without --discard")
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; strings.Contains(out, "T1") {
		t.Errorf("a cancelled task is still listed: %s", out)
	}
	if r := csq(t, h, opts{env: mine()}, "cancel", "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is already cancelled; its work is kept (worktree "+wt) || !strings.Contains(r.stderr, "csquad cancel T1 --discard deletes it") {
		t.Errorf("cancelling twice: %+v", r)
	}
	// Numbering continues past a cancelled task.
	if n := launchAs(t, h, sid, "next"); n != "2" {
		t.Errorf("next number %s", n)
	}
	// The command cancel printed deletes the kept work, and only the task's.
	git(t, h, repo, "branch", "user-work")
	git(t, h, repo, "branch", "worktree-t2-next")
	r = csq(t, h, opts{env: mine("FAKE_RM_REFUSE=1")}, "cancel", "T1", "--discard")
	if r.code != 0 || !strings.Contains(r.stdout, "discarded T1 · fix · ") || !strings.Contains(r.stdout, "branch worktree-t1-fix") || !strings.Contains(r.stdout, "removed session") {
		t.Fatalf("--discard after cancel: %+v", r)
	}
	branches := git(t, h, repo, "branch")
	if exists(wt) || strings.Contains(branches, "worktree-t1-fix") || !strings.Contains(branches, "user-work") || !strings.Contains(branches, "worktree-t2-next") {
		t.Errorf("refs after --discard: %s", branches)
	}
	for _, s := range h.Sessions() {
		if strings.HasPrefix(s.Name, "T1 · ") {
			t.Error("the session of T1 is left")
		}
	}
	if r := csq(t, h, opts{env: mine()}, "cancel", "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is already cancelled") || strings.Contains(r.stderr, "--discard") {
		t.Errorf("nothing left: %+v", r)
	}
	if r := csq(t, h, opts{env: mine()}, "cancel", "T1", "--discard"); r.code != 0 || !strings.Contains(r.stdout, "discarded T1 · fix · nothing") {
		t.Errorf("--discard twice: %+v", r)
	}
	if exists(filepath.Join(h.Csquad, "logs", "T1.lock")) {
		t.Error("the lock file is left")
	}

	h2, repo2, wt2 := repoWithTask(t)
	ok(t, csq(t, h2, opts{env: mine()}, "cancel", "T1", "--discard"))
	if exists(wt2) || strings.Contains(git(t, h2, repo2, "branch"), "worktree-t1-fix") || len(h2.Sessions()) != 0 {
		t.Error("--discard keeps the task's refs")
	}
	if !strings.Contains(git(t, h2, repo2, "log", "--oneline", "main"), "init") {
		t.Error("main touched")
	}
}

// commit writes a file in dir and commits it.
func commit(t *testing.T, h *testutil.Home, dir, file, msg string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, file), []byte(msg+"\n"), 0o644)
	git(t, h, dir, "add", file)
	git(t, h, dir, "commit", "-qm", msg)
}

// The user cherry-picked two of the task's four commits and left
// screenshots in the worktree: finish names what blocks it and both ways
// out.
func TestFinishRefusalNamesWhatBlocksAndBothWaysOut(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	commit(t, h, wt, "c.txt", "second")
	commit(t, h, wt, "d.txt", "third")
	commit(t, h, wt, "e.txt", "fourth")
	git(t, h, repo, "cherry-pick", "worktree-t1-fix~2", "worktree-t1-fix")
	os.MkdirAll(filepath.Join(wt, ".pristine", "deep"), 0o755)
	for _, f := range []string{"a.png", "b.png", filepath.Join("deep", "c.png")} {
		os.WriteFile(filepath.Join(wt, ".pristine", f), []byte("x"), 0o644)
	}
	fix, third := git(t, h, repo, "rev-parse", "--short", "worktree-t1-fix~3"), git(t, h, repo, "rev-parse", "--short", "worktree-t1-fix~1")
	ways := "  keep the work and stop tracking it: csquad cancel T1\n" +
		"  delete it:                          csquad cancel T1 --discard\n"
	branch := "csquad: T1 is not finished, nothing was removed:\n" +
		"  - branch worktree-t1-fix is not merged into main:\n" +
		"      " + fix + " fix\n" +
		"      " + third + " third\n" +
		"      2 commits already in main by patch (cherry-picked)\n"
	r := csq(t, h, opts{env: mine()}, "finish", "T1")
	if want := branch + "  - worktree " + wt + " has uncommitted changes: only untracked files: .pristine/ (3 files)\n" + ways; r.code != 1 || r.stderr != want {
		t.Errorf("untracked only:\n%s\nwant:\n%s", r.stderr, want)
	}
	// Modified tracked files are told apart; the list is capped.
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("changed\n"), 0o644)
	for _, f := range []string{"n1", "n2", "n3", "n4", "n5"} {
		os.WriteFile(filepath.Join(wt, f), []byte("x"), 0o644)
	}
	r = csq(t, h, opts{env: mine()}, "finish", "T1")
	if want := branch + "  - worktree " + wt + " has uncommitted changes: modified tracked files: b.txt; untracked files: .pristine/ (3 files), n1, n2, n3, n4, and 1 more\n" + ways; r.code != 1 || r.stderr != want {
		t.Errorf("modified:\n%s\nwant:\n%s", r.stderr, want)
	}
	if !exists(wt) || len(h.Sessions()) != 1 || !strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") {
		t.Error("something was removed")
	}
}

// A branch whose commits are all in the target by patch (cherry-picked or
// rebased) is merged; a cherry-pick whose patch changed is not.
func TestFinishTreatsABranchInTheTargetByPatchAsMerged(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	commit(t, h, repo, "m.txt", "main moves on")
	git(t, h, repo, "cherry-pick", "worktree-t1-fix")
	// A detached worktree of the task at that commit follows the same rule.
	probe := filepath.Join(repo, ".claude", "worktrees", "t1-probe")
	git(t, h, repo, "worktree", "add", "-q", "--detach", probe, "worktree-t1-fix")
	r := ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if !strings.Contains(r.stdout, "finished T1 · fix · merged into main by patch (cherry-picked/rebased): worktree-t1-fix · removed ") {
		t.Errorf("cherry-picked: %s", r.stdout)
	}
	if exists(wt) || exists(probe) || strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") || len(h.Sessions()) != 0 {
		t.Error("cherry-picked: not cleaned up")
	}

	h, repo, wt = repoWithTask(t)
	commit(t, h, wt, "c.txt", "second")
	commit(t, h, repo, "m.txt", "main moves on")
	git(t, h, repo, "branch", "rebased", "worktree-t1-fix")
	git(t, h, repo, "rebase", "-q", "main", "rebased")
	git(t, h, repo, "switch", "-q", "main")
	git(t, h, repo, "merge", "-q", "--ff-only", "rebased")
	git(t, h, repo, "branch", "-q", "-d", "rebased")
	r = ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if !strings.Contains(r.stdout, "merged into main by patch (cherry-picked/rebased): worktree-t1-fix") || exists(wt) || strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") {
		t.Errorf("rebased: %s", r.stdout)
	}

	// Resolved differently while picking: the patch changed, so unmerged.
	h, repo, wt = repoWithTask(t)
	git(t, h, repo, "cherry-pick", "-n", "worktree-t1-fix")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b, resolved\n"), 0o644)
	git(t, h, repo, "commit", "-qam", "fix, resolved")
	r = csq(t, h, opts{env: mine()}, "finish", "T1")
	if r.code != 1 || !strings.Contains(r.stderr, "branch worktree-t1-fix is not merged into main:\n      ") || strings.Contains(r.stderr, "by patch") || !exists(wt) {
		t.Errorf("changed patch: %+v", r)
	}
}
