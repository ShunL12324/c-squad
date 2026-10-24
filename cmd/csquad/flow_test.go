package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/flock"
	"github.com/ShunL12324/c-squad/internal/store"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

// ---- helpers ----------------------------------------------------------------

func openDB(t *testing.T, h *testutil.Home) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(h.Csquad, "csquad.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func getTask(t *testing.T, h *testutil.Home, id int) store.Task {
	t.Helper()
	db := openDB(t, h)
	task, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	return task
}

// setTask rewrites a task's flow fields, as if it had moved there.
func setTask(t *testing.T, h *testutil.Home, id int, f func(*store.Task)) store.Task {
	t.Helper()
	db := openDB(t, h)
	cur, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	next := cur
	f(&next)
	stored, err := db.Transition(context.Background(), cur, next)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	return stored
}

func waitUntil(t *testing.T, what string, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitPhase(t *testing.T, h *testutil.Home, id int, phase string) store.Task {
	t.Helper()
	var task store.Task
	waitUntil(t, fmt.Sprintf("T%d %s", id, phase), 20*time.Second, func() bool {
		task = getTask(t, h, id)
		return task.Phase == phase
	})
	return task
}

// events are the event log's lines as "T1 from→to".
func events(h *testutil.Home) []string {
	f, err := os.Open(filepath.Join(h.Csquad, "logs", "events.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e struct{ Task, From, To, Reason, Time string }
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e.Task+" "+e.From+"→"+e.To)
		}
	}
	return out
}

// flowRepo is a repository on main with one commit, and a git identity in
// HOME (detached csquad processes run without GIT_* variables).
func flowRepo(t *testing.T) (*testutil.Home, string) {
	t.Helper()
	h := testutil.NewHome(t)
	h.ExtraEnv = gitIdentity
	os.WriteFile(filepath.Join(h.Home, ".gitconfig"), []byte("[user]\n\tname = t\n\temail = t@example.com\n"), 0o644)
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(repo, 0o755)
	git(t, h, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "init")
	return h, repo
}

func launchIn(t *testing.T, h *testutil.Home, repo, title string, extra ...string) int {
	t.Helper()
	r := ok(t, csq(t, h, opts{stdin: "do " + title, env: mine(), dir: repo}, append([]string{"launch", "--title", title, "--cwd", repo}, extra...)...))
	m := regexp.MustCompile(`^(?:launched|queued) T(\d+) `).FindStringSubmatch(r.stdout)
	if m == nil {
		t.Fatalf("launch: %q", r.stdout)
	}
	var n int
	fmt.Sscan(m[1], &n)
	return n
}

// work plays task n's worker: its worktree t<n>-fix on worktree-t<n>-fix
// (made on first use), files committed there, the session idle in it.
func work(t *testing.T, h *testutil.Home, repo string, n int, files map[string]string) string {
	t.Helper()
	wt := filepath.Join(repo, ".claude", "worktrees", fmt.Sprintf("t%d-fix", n))
	if !exists(wt) {
		git(t, h, repo, "worktree", "add", "-q", "-b", fmt.Sprintf("worktree-t%d-fix", n), wt)
		git(t, h, repo, "worktree", "lock", wt)
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(wt, name), []byte(content), 0o644)
	}
	git(t, h, wt, "add", ".")
	git(t, h, wt, "commit", "-qm", "work")
	setState(h, fmt.Sprint(n), func(s *testutil.Session) { s.State, s.Status, s.Cwd = "working", "idle", wt })
	return wt
}

// reply writes the worker's transcript: csquad's message, then its answer,
// dated now.
func reply(t *testing.T, h *testutil.Home, n int, text string) {
	t.Helper()
	now := time.Now().UTC()
	writeTranscript(t, h, fmt.Sprint(n),
		fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":"csquad: ..."}}`, now.Add(-time.Second).Format(time.RFC3339Nano)),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"content":[{"type":"text","text":%q}]}}`, now.Format(time.RFC3339Nano), text))
}

func advance(t *testing.T, h *testutil.Home, env ...string) result {
	t.Helper()
	return ok(t, csq(t, h, opts{env: append([]string{"CSQUAD_SETTLE_MS=0"}, env...)}, "advance"))
}

// sent are the messages csquad sent the fake sessions (claude --bg --resume).
func sent(h *testutil.Home) []string {
	var out []string
	for _, a := range h.Argv() {
		if testutil.Arg(a, "--resume") != "" {
			out = append(out, a[len(a)-1])
		}
	}
	return out
}

func brief(t store.Task) string {
	return fmt.Sprintf("%s: %s/%s round %d/%d session %q note %q last_check %s", t.Label(), t.Phase, t.Step, t.Round, t.MaxRounds, t.SessionID, t.Note, t.LastCheck)
}

func branches(t *testing.T, h *testutil.Home, repo string) string {
	return git(t, h, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
}

// ---- the check and fix loop -------------------------------------------------

// A cmd: check fails, the findings go to the worker, its reply starts the
// next round, the check passes and the merge lands: rebase, fast-forward,
// cleanup, done.
func TestCheckFixLoopThenMerge(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:test -f hello.txt", "--merge")
	if task := getTask(t, h, n); task.MergeInto != "main" || task.CheckSpec != "cmd:test -f hello.txt" || task.MaxRounds != 3 || !strings.Contains(task.WorkerRules, "starting with `csquad:`") {
		t.Fatalf("%s", brief(task))
	}
	wt := work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	// Not settled yet: the default 10 s.
	ok(t, csq(t, h, opts{}, "advance"))
	if p := getTask(t, h, n).Phase; p != store.Running {
		t.Fatalf("moved before the session settled: %s", p)
	}
	advance(t, h)
	task := waitPhase(t, h, n, store.Fixing)
	if task.Round != 2 || !strings.Contains(task.LastCheck, `"verdict":"fail"`) {
		t.Errorf("%s", brief(task))
	}
	waitUntil(t, "the findings sent", 10*time.Second, func() bool { return len(sent(h)) == 1 })
	if msg := sent(h)[0]; !strings.HasPrefix(msg, "csquad: check 1 of 3 (cmd:test -f hello.txt) did not pass. `test -f hello.txt` exited 1") {
		t.Errorf("message %q", msg)
	}
	// Settled idle is not a reply in fixing: right after the message the
	// session still reads idle.
	advance(t, h)
	if p := getTask(t, h, n).Phase; p != store.Fixing {
		t.Fatalf("fixing moved without a reply: %s", p)
	}
	work(t, h, repo, n, map[string]string{"hello.txt": "hi\n"})
	reply(t, h, n, "Added hello.txt.")
	advance(t, h)
	task = waitPhase(t, h, n, store.Done)
	if git(t, h, repo, "show", "main:hello.txt") != "hi" {
		t.Error("main lacks the work")
	}
	if exists(wt) || strings.Contains(branches(t, h, repo), "worktree-t1-fix") || len(h.Sessions()) != 0 {
		t.Errorf("not cleaned up: %s %v", branches(t, h, repo), h.Sessions())
	}
	var oc struct {
		Target   string
		Branches []string
		Final    string
	}
	json.Unmarshal([]byte(task.Outcome), &oc)
	if oc.Target != "main" || len(oc.Branches) != 1 || oc.Branches[0] != "worktree-t1-fix" || oc.Final != "Added hello.txt." {
		t.Errorf("outcome %s", task.Outcome)
	}
	want := "T1 running→checking,T1 checking→fixing,T1 fixing→checking,T1 checking→merging,T1 merging→done"
	if got := strings.Join(events(h), ","); got != want {
		t.Errorf("events %s", got)
	}
	logs, _ := filepath.Glob(filepath.Join(h.Csquad, "logs", "T1-check-*.log"))
	if len(logs) != 2 {
		t.Errorf("check logs %v", logs)
	}
	if exists(filepath.Join(h.Csquad, "settings", "T1.json")) || exists(filepath.Join(h.Csquad, "logs", "T1.lock")) {
		t.Error("the files of a done task are left")
	}
}

// Four processes advance the same task at once: the check is spawned once.
func TestConcurrentAdvanceRunsTheEffectOnce(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:sleep 1")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// status advances first, without advance's own lock. A slow
			// `claude agents` makes all four read the task before any writes.
			csq(t, h, opts{env: []string{"CSQUAD_SETTLE_MS=0", "FAKE_AGENTS_SLEEP=700"}}, "status", "--all")
		}()
	}
	wg.Wait()
	waitPhase(t, h, n, store.Ready)
	logs, _ := filepath.Glob(filepath.Join(h.Csquad, "logs", "T1-check-*.log"))
	spawned := 0
	for _, l := range logs {
		b, _ := os.ReadFile(l)
		spawned += strings.Count(string(b), "csquad check T1\n")
	}
	if spawned != 1 {
		t.Errorf("the check was spawned %d times (%v)", spawned, logs)
	}
	if got := strings.Join(events(h), ","); got != "T1 running→checking,T1 checking→ready" {
		t.Errorf("events %s", got)
	}
}

// A cmd: check runs with the allowlisted environment, not the caller's.
func TestCheckEnvironmentIsAllowlisted(t *testing.T) {
	h, repo := flowRepo(t)
	out := filepath.Join(h.Root, "env.txt")
	// On Windows the check runs in Git Bash: the path takes forward slashes,
	// and pwd -W prints the directory as Windows names it.
	pwd, sh := "pwd", out
	if runtime.GOOS == "windows" {
		pwd, sh = "pwd -W", filepath.ToSlash(out)
	}
	n := launchIn(t, h, repo, "fix", "--check", "cmd:env > "+sh+"; "+pwd+" >> "+sh)
	wt := work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	advance(t, h, "ANTHROPIC_AUTH_TOKEN=sk-secret", "SOME_VAR=x")
	waitPhase(t, h, n, store.Ready)
	b, _ := os.ReadFile(out)
	env := string(b)
	if strings.Contains(env, "sk-secret") || strings.Contains(env, "SOME_VAR") || !strings.Contains(env, "PATH=") || !strings.Contains(env, "HOME="+h.Home) {
		t.Errorf("environment:\n%s", env)
	}
	if !strings.HasSuffix(strings.TrimSpace(env), filepath.ToSlash(wt)) {
		t.Errorf("not run in the worktree:\n%s", env)
	}
}

// A Claude reviewer's verdict drives the loop; at max rounds the task needs
// you, and retry runs one more round.
func TestReviewerMaxRoundsAndRetry(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "claude:opus", "--max", "1")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	h.SetReview(`{"type":"result","is_error":false,"result":"","structured_output":{"verdict":"fail","summary":"no tests","findings":[{"severity":"blocking","file":"b.txt","summary":"add a test"}]}}`)
	advance(t, h)
	task := waitPhase(t, h, n, store.NeedsYou)
	if task.Step != "round" || !strings.HasPrefix(task.Note, "max rounds: 1 of 1 checks did not pass (no tests)") {
		t.Errorf("%s", brief(task))
	}
	prompts := h.ReviewPrompts()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Review branch worktree-t1-fix") || !strings.Contains(prompts[0], "git diff main...HEAD") {
		t.Errorf("review prompt %q", prompts)
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; !strings.Contains(out, "state: needs you ·") || !strings.Contains(out, "note: max rounds: 1 of 1") {
		t.Errorf("status:\n%s", out)
	}
	r := ok(t, csq(t, h, opts{env: mine()}, "retry", "T1"))
	if !strings.Contains(r.stdout, "retry: one more round (2 of 2)") {
		t.Errorf("%s", r.stdout)
	}
	task = getTask(t, h, n)
	if task.Phase != store.Fixing || task.Round != 2 || task.MaxRounds != 2 {
		t.Errorf("%s", brief(task))
	}
	if msgs := sent(h); len(msgs) != 1 || !strings.Contains(msgs[0], "- [blocking] b.txt: add a test") {
		t.Errorf("sent %q", msgs)
	}
	if r := csq(t, h, opts{env: mine()}, "retry", "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is fixing") {
		t.Errorf("%+v", r)
	}
}

// No verdict, and a check that died: needs you, never retried by itself.
func TestNoVerdictNeedsYou(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "claude")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	h.SetReview(`{"type":"result","is_error":false,"result":"Looks fine I guess."}`)
	advance(t, h)
	task := waitPhase(t, h, n, store.NeedsYou)
	if task.Step != "check" || !strings.HasPrefix(task.Note, "no verdict: the Claude reviewer gave no verdict") {
		t.Errorf("%s", brief(task))
	}
	// retry runs the check again, now with a verdict.
	h.SetReview(`{"type":"result","is_error":false,"result":"{\"verdict\":\"pass\",\"summary\":\"ok\",\"findings\":[]}"}`)
	ok(t, csq(t, h, opts{env: mine()}, "retry", "T1"))
	waitPhase(t, h, n, store.Ready)

	// A check whose process is gone (its lock free, no verdict) for the grace period.
	n2 := launchIn(t, h, repo, "other", "--check", "claude")
	setState(h, fmt.Sprint(n2), func(s *testutil.Session) { s.Status = "idle" })
	setTask(t, h, n2, func(t *store.Task) { t.Phase, t.Round = store.Checking, 1 })
	advance(t, h, "CSQUAD_CHECK_GRACE_MS=60000")
	if p := getTask(t, h, n2).Phase; p != store.Checking {
		t.Fatalf("within the grace period: %s %v", p, events(h))
	}
	advance(t, h, "CSQUAD_CHECK_GRACE_MS=0")
	if task := getTask(t, h, n2); task.Phase != store.NeedsYou || !strings.Contains(task.Note, "the check stopped without one") {
		t.Errorf("%s", brief(task))
	}
}

// ---- merging ------------------------------------------------------------------

// The target moved on without conflicts: csquad rebases the branch itself
// and fast-forwards main in the main checkout.
func TestMergeRebasesCleanly(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--merge")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "main moved")
	advance(t, h)
	if task := getTask(t, h, n); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	if log := git(t, h, repo, "log", "--format=%s", "main"); log != "work\nmain moved\ninit" {
		t.Errorf("history %q", log)
	}
	if !exists(filepath.Join(repo, "b.txt")) || len(sent(h)) != 0 {
		t.Error("the main checkout was not fast-forwarded, or the worker was messaged")
	}
}

// A conflict: csquad aborts its rebase and asks the worker; the worker's
// rebase is landed; a second conflict needs you.
func TestMergeConflictGoesToTheWorker(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--merge")
	wt := work(t, h, repo, n, map[string]string{"a.txt": "task\n"})
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("main\n"), 0o644)
	git(t, h, repo, "commit", "-qam", "main moved")
	advance(t, h)
	task := getTask(t, h, n)
	if task.Phase != store.Merging || task.Step != "asked" {
		t.Fatalf("%s", brief(task))
	}
	if git(t, h, wt, "status", "--porcelain") != "" || exists(filepath.Join(wt, ".git", "rebase-merge")) {
		t.Error("csquad's rebase was not aborted")
	}
	msgs := sent(h)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "In your worktree "+wt+" run `git rebase main`") || !strings.Contains(msgs[0], "CONFLICT") {
		t.Fatalf("sent %q", msgs)
	}
	// Not yet: the worker has not answered.
	advance(t, h)
	if p := getTask(t, h, n).Phase; p != store.Merging {
		t.Fatal(p)
	}
	// The worker rebases, resolving the conflict.
	gitNoFail(h, wt, "rebase", "main")
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("main\ntask\n"), 0o644)
	git(t, h, wt, "add", "a.txt")
	git(t, h, wt, "-c", "core.editor=true", "rebase", "--continue")
	reply(t, h, n, "Rebased onto main.")
	advance(t, h)
	if task := getTask(t, h, n); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "main\ntask\n" {
		t.Errorf("a.txt %q", b)
	}

	// The worker's answer does not resolve it: needs you.
	n2 := launchIn(t, h, repo, "again", "--merge")
	work(t, h, repo, n2, map[string]string{"a.txt": "other\n"})
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("main again\n"), 0o644)
	git(t, h, repo, "commit", "-qam", "main moved again")
	advance(t, h)
	reply(t, h, n2, "I could not.")
	advance(t, h)
	if task := getTask(t, h, n2); task.Phase != store.NeedsYou || task.Step != "merge" || !strings.HasPrefix(task.Note, "merge conflict: worktree-t2-fix still does not rebase onto main") {
		t.Errorf("%s", brief(task))
	}
	if !strings.Contains(git(t, h, repo, "log", "--format=%s", "-1", "main"), "main moved again") {
		t.Error("main moved")
	}
}

func gitNoFail(h *testutil.Home, dir string, args ...string) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = h.Env()
	cmd.Run()
}

// The target is checked out nowhere: csquad moves its ref (fetch . b:t).
func TestMergeFastForwardsARefCheckedOutNowhere(t *testing.T) {
	h, repo := flowRepo(t)
	git(t, h, repo, "branch", "release")
	n := launchIn(t, h, repo, "fix", "--merge", "release")
	if m := getTask(t, h, n).MergeInto; m != "release" {
		t.Fatal(m)
	}
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	if task := getTask(t, h, n); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	if git(t, h, repo, "log", "--format=%s", "-1", "release") != "work" || git(t, h, repo, "log", "--format=%s", "-1", "main") != "init" {
		t.Error("release not fast-forwarded, or main moved")
	}
	if r := launch(t, h, "x", "--merge", "nope"); r.code != 1 || !strings.Contains(r.stderr, "--merge needs a git repository") {
		t.Errorf("%+v", r)
	}
	if r := csq(t, h, opts{stdin: "x", dir: repo}, "launch", "--title", "x", "--cwd", repo, "--merge", "nope"); r.code != 1 || !strings.Contains(r.stderr, "there is no branch nope") {
		t.Errorf("%+v", r)
	}
	if r := csq(t, h, opts{stdin: "x", dir: repo}, "launch", "--title", "x", "--cwd", repo, "--merge", "--isolation", "none"); r.code != 1 || !strings.Contains(r.stderr, "--merge needs a worktree task") {
		t.Errorf("%+v", r)
	}
}

// What csquad cannot prove or do: needs you, nothing removed.
func TestUnprovenMergeNeedsYou(t *testing.T) {
	h, repo := flowRepo(t)
	// A dirty worktree.
	n := launchIn(t, h, repo, "fix", "--merge")
	wt := work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("x\n"), 0o644)
	advance(t, h)
	task := getTask(t, h, n)
	if task.Phase != store.NeedsYou || task.Step != "merge" || !strings.Contains(task.Note, "has uncommitted changes") {
		t.Errorf("%s", brief(task))
	}
	if !exists(wt) || git(t, h, repo, "log", "--format=%s", "-1", "main") != "init" {
		t.Error("something was changed")
	}
	// The fast-forward is refused: the main checkout has local changes to the same file.
	os.Remove(filepath.Join(wt, "dirty.txt"))
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("local\n"), 0o644)
	ok(t, csq(t, h, opts{env: mine()}, "retry", "T1"))
	task = getTask(t, h, n)
	if task.Phase != store.NeedsYou || !strings.Contains(task.Note, "fast-forward of main in "+repo+" refused") {
		t.Errorf("%s", brief(task))
	}
	if !exists(wt) || len(h.Sessions()) != 1 {
		t.Error("cleaned up after a refused fast-forward")
	}
	// No branch at all.
	os.Remove(filepath.Join(repo, "b.txt"))
	n2 := launchIn(t, h, repo, "empty", "--merge")
	setState(h, fmt.Sprint(n2), func(s *testutil.Session) { s.State, s.Status = "working", "idle" })
	advance(t, h)
	if task := getTask(t, h, n2); task.Phase != store.NeedsYou || !strings.Contains(task.Note, "no branch of T2 found") {
		t.Errorf("%s", brief(task))
	}
}

// ---- dependencies ---------------------------------------------------------------

// T2 --after T1 waits; when T1's merge lands, T2 starts with what T1 left.
// ready does not release it, finish does; a cancelled dependency stops it.
func TestDependencyRelease(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first", "--merge")
	if r := csq(t, h, opts{stdin: "x", dir: repo}, "launch", "--title", "x", "--after", "T1,T9"); r.code != 1 || !strings.Contains(r.stderr, "T9 is not a csquad task") {
		t.Errorf("%+v", r)
	}
	r := ok(t, csq(t, h, opts{stdin: "build on it", env: mine(), dir: repo}, "launch", "--title", "second", "--cwd", repo, "--after", "T1"))
	if !strings.HasPrefix(r.stdout, "queued T2 · second · after T1 · ") {
		t.Errorf("%s", r.stdout)
	}
	if task := getTask(t, h, 2); task.Phase != store.Waiting || task.SessionID != "" || len(h.Sessions()) != 1 {
		t.Fatalf("%s", brief(task))
	}
	if rows := statusline(t, h, sid); !regexp.MustCompile(`◌ T2 (✻ )?second ·+ +waiting T1`).MatchString(strings.Join(rows, "\n")) {
		t.Errorf("ledger %q", rows)
	}
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	writeTranscript(t, h, "1", `{"type":"assistant","message":{"content":[{"type":"text","text":"Added b.txt on worktree-t1-fix."}]}}`)
	advance(t, h)
	if p := getTask(t, h, t1).Phase; p != store.Done {
		t.Fatal(p)
	}
	t2 := waitPhase(t, h, 2, store.Running)
	if t2.SessionID == "" {
		t.Fatal("T2 has no session")
	}
	var prompt string
	for _, a := range h.Argv() {
		if testutil.Arg(a, "--name") == "T2 · second" {
			prompt = a[len(a)-1]
		}
	}
	if !strings.HasPrefix(prompt, "build on it\n\n---\nThis task was queued to start after T1") || !strings.Contains(prompt, "T1 · first: merged worktree-t1-fix into main.") ||
		!strings.Contains(prompt, "Added b.txt on worktree-t1-fix.") {
		t.Errorf("prompt %q", prompt)
	}

	// ready does not release; finish does.
	t3 := launchIn(t, h, repo, "third", "--check", "cmd:true")
	launchIn(t, h, repo, "fourth", "--after", fmt.Sprintf("T%d", t3))
	work(t, h, repo, t3, map[string]string{"c.txt": "c\n"})
	advance(t, h)
	waitPhase(t, h, t3, store.Ready)
	advance(t, h)
	if p := getTask(t, h, 4).Phase; p != store.Waiting {
		t.Fatalf("ready released a dependent: %s", p)
	}
	git(t, h, repo, "merge", "-q", "--ff-only", "worktree-t3-fix")
	ok(t, csq(t, h, opts{env: mine()}, "finish", "T3"))
	if p := getTask(t, h, 4).Phase; p != store.Running {
		t.Errorf("finish did not release T4: %s", p)
	}

	// A cancelled dependency: the dependent needs you; retry waits again.
	launchIn(t, h, repo, "fifth", "--after", "T2")
	ok(t, csq(t, h, opts{env: mine()}, "cancel", "T2"))
	task := getTask(t, h, 5)
	if task.Phase != store.NeedsYou || task.Step != "waiting" || !strings.Contains(task.Note, "dependency gone: T2 was cancelled") {
		t.Errorf("%s", brief(task))
	}
	if r := csq(t, h, opts{stdin: "x", dir: repo}, "launch", "--title", "x", "--after", "T2"); r.code != 1 || !strings.Contains(r.stderr, "T2 was cancelled") {
		t.Errorf("%+v", r)
	}
	if r := csq(t, h, opts{env: mine()}, "finish", "T5"); r.code != 1 {
		t.Errorf("finish of a task that never started: %+v", r)
	}
}

// A queued task starts from a process without the launching session's
// environment: its provider settings were merged at launch.
func TestQueuedTaskKeepsTheLaunchingSessionsProvider(t *testing.T) {
	h, repo := flowRepo(t)
	launchIn(t, h, repo, "first", "--merge")
	r := ok(t, csq(t, h, opts{stdin: "next", dir: repo, env: mine("ANTHROPIC_BASE_URL=https://relay.example", "ANTHROPIC_AUTH_TOKEN=sk-q")},
		"launch", "--title", "second", "--cwd", repo, "--after", "T1", "--", "--settings", `{"model":"opus"}`))
	if !strings.Contains(r.stderr, "T2 will use this session's provider settings (ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL)") {
		t.Errorf("%s", r.stderr)
	}
	if args := strings.Join(getTask(t, h, 2).EngineArgs, " "); args != "--settings "+filepath.Join(h.Csquad, "settings", "T2.json") || strings.Contains(args, "sk-q") {
		t.Errorf("engine args %q", args)
	}
	work(t, h, repo, 1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	waitPhase(t, h, 2, store.Running)
	argv := h.LastArgv()
	if testutil.Arg(argv, "--name") != "T2 · second" || strings.Count(strings.Join(argv, " "), "--settings") != 1 {
		t.Fatalf("%q", argv)
	}
	jsonEq(t, settingsOf(t, argv), `{"model":"opus","worktree":{"baseRef":"head"},"env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-q"}}`)
}

// Landing a merge holds the task's lock: cancel and finish refuse meanwhile,
// and a landing that finds the lock held leaves the task to its holder.
func TestTaskLockKeepsCancelFinishAndLandingApart(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--merge")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	l, err := flock.Try(filepath.Join(h.Csquad, "logs", "T1.lock"))
	if err != nil {
		t.Fatal(err)
	}
	advance(t, h)
	if task := getTask(t, h, n); task.Phase != store.Merging || git(t, h, repo, "log", "--format=%s", "-1", "main") != "init" {
		t.Fatalf("landed although another process holds the lock: %s", brief(task))
	}
	for _, verb := range []string{"cancel", "finish"} {
		if r := csq(t, h, opts{env: mine()}, verb, "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is busy") {
			t.Errorf("%s: %+v", verb, r)
		}
	}
	l.Release()
	// The holder is gone and the landing never finished: after the grace
	// period the next advance lands it.
	advance(t, h, "CSQUAD_CHECK_GRACE_MS=0")
	if task := getTask(t, h, n); task.Phase != store.Done {
		t.Errorf("%s", brief(task))
	}
}

// wait drives the flow on each poll and does not stop at an idle worker
// between its steps: it reports ready, or needs you with the reason.
func TestWaitFollowsTheFlow(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:sleep 0.3")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	r := waitFor(t, h, []string{"CSQUAD_SETTLE_MS=0"}, nil, "T1")
	if r.code != 0 || r.stdout != "T1 · fix · ready\n" {
		t.Errorf("%+v", r)
	}
	n2 := launchIn(t, h, repo, "other", "--check", "cmd:false", "--max", "1")
	work(t, h, repo, n2, map[string]string{"c.txt": "c\n"})
	r = waitFor(t, h, []string{"CSQUAD_SETTLE_MS=0"}, nil, "T2")
	if !strings.HasPrefix(r.stdout, "T2 · other · needs you · max rounds: 1 of 1 checks did not pass") {
		t.Errorf("%+v", r)
	}
}

// ---- hooks -----------------------------------------------------------------------

func TestClaudeStopHookAdvancesTheTask(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:true")
	argv := h.LastArgv()
	cmd := ""
	{
		file := testutil.Arg(argv, "--settings")
		b, _ := os.ReadFile(file)
		var m struct {
			Hooks struct {
				Stop []struct{ Hooks []struct{ Command string } }
			}
		}
		json.Unmarshal(b, &m)
		cmd = m.Hooks.Stop[len(m.Hooks.Stop)-1].Hooks[0].Command
	}
	if !strings.HasSuffix(cmd, " hook --home "+assets.ShellPath(h.Csquad, runtime.GOOS == "windows")+" claude-stop") {
		t.Errorf("hook command %q", cmd)
	}
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	full := ""
	for _, s := range h.Sessions() {
		full = s.SessionID
	}
	// A session that is no task: nothing happens.
	hookEnv := []string{"CSQUAD_HOME=", "CSQUAD_SETTLE_MS=600000", "CSQUAD_STOP_POLL_MS=20"}
	r := ok(t, csq(t, h, opts{stdin: `{"session_id":"ffffffff-0000-4000-8000-000000000000","hook_event_name":"Stop"}`, env: hookEnv}, "hook", "--home", h.Csquad, "claude-stop"))
	if r.stdout != "" || exists(filepath.Join(h.Csquad, "logs", "advance.log")) {
		t.Errorf("a stranger's Stop: %+v", r)
	}
	// The task's session: a detached advance counts the turn as finished at
	// once (no settle time), here with the home given only by --home.
	start := time.Now()
	r = ok(t, csq(t, h, opts{stdin: `{"session_id":"` + full + `","hook_event_name":"Stop","stop_hook_active":false}`, env: hookEnv}, "hook", "--home", h.Csquad, "claude-stop"))
	if r.stdout != "" || time.Since(start) > 3*time.Second {
		t.Errorf("the hook must return at once: %+v", r)
	}
	waitPhase(t, h, n, store.Ready)
	b, _ := os.ReadFile(filepath.Join(h.Csquad, "logs", "advance.log"))
	if !strings.Contains(string(b), "csquad advance --stopped T1") {
		t.Errorf("advance.log:\n%s", b)
	}
}

func TestCodexStopHookSpawnsAdvance(t *testing.T) {
	h := testutil.NewHome(t)
	db := openDB(t, h)
	db.Insert(context.Background(), store.Task{Title: "codex job", Cwd: h.Root, Engine: "codex", Prompt: "p", Phase: store.Running, SessionID: "019a-thread", MergeInto: "main"})
	db.Close()
	env := []string{"CSQUAD_STOP_WAIT_MS=0"}
	ok(t, csq(t, h, opts{stdin: `{"session_id":"other-thread"}`, env: env}, "hook", "codex-stop"))
	if exists(filepath.Join(h.Csquad, "logs", "advance.log")) {
		t.Error("a thread that is no task")
	}
	// advance.log moves to advance.log.1 past 1 MiB.
	log := filepath.Join(h.Csquad, "logs", "advance.log")
	os.MkdirAll(filepath.Dir(log), 0o700)
	os.WriteFile(log, bytes.Repeat([]byte("x"), 1<<20+1), 0o600)
	ok(t, csq(t, h, opts{stdin: `{"session_id":"019a-thread","turn_id":"t","last_assistant_message":"done"}`, env: env}, "hook", "codex-stop"))
	waitUntil(t, "advance --stopped T1", 10*time.Second, func() bool {
		b, _ := os.ReadFile(log)
		return strings.Contains(string(b), "csquad advance --stopped T1")
	})
	if st, err := os.Stat(log + ".1"); err != nil || st.Size() != 1<<20+1 {
		t.Errorf("advance.log was not rotated: %v", err)
	}
	if st, _ := os.Stat(log); st.Size() > 1<<16 {
		t.Errorf("advance.log is %d bytes after rotation", st.Size())
	}
	if r := csq(t, h, opts{stdin: "{}"}, "hook", "other-stop"); r.code != 1 || !strings.Contains(r.stderr, "unknown hook") {
		t.Errorf("%+v", r)
	}
	if r := ok(t, csq(t, h, opts{stdin: "not json"}, "hook", "claude-stop")); r.stdout != "" {
		t.Errorf("%+v", r)
	}
}

// ---- status line ---------------------------------------------------------------

func TestLedgerRowsForFlowPhases(t *testing.T) {
	h := testutil.NewHome(t)
	for _, title := range []string{"checking", "fixing", "merging", "needs", "plain"} {
		launchAs(t, h, sid, title)
	}
	ok(t, csq(t, h, opts{stdin: "x", env: mine()}, "launch", "--title", "queued", "--cwd", h.Root, "--after", "T1,T2"))
	setTask(t, h, 1, func(t *store.Task) { t.Phase, t.Round, t.MaxRounds, t.CheckSpec = store.Checking, 1, 3, "codex" })
	setTask(t, h, 2, func(t *store.Task) { t.Phase, t.Round, t.MaxRounds, t.CheckSpec = store.Fixing, 2, 3, "codex" })
	setTask(t, h, 3, func(t *store.Task) { t.Phase, t.MergeInto = store.Merging, "main" })
	setState(h, "4", func(s *testutil.Session) { s.Status = "idle" })
	setTask(t, h, 4, func(t *store.Task) {
		t.Phase, t.Step, t.Note, t.CheckSpec = store.NeedsYou, "merge", "merge conflict: worktree-t4-x still does not rebase onto main", "codex"
	})
	rows := statusline(t, h, sid, "COLUMNS=240", "CSQUAD_CHECK_GRACE_MS=600000")
	text := strings.Join(rows, "\n")
	for _, want := range []string{`◆ T4 +(✻ )?needs ·+ +needs you · merge conflict `, `◐ T1 +(✻ )?checking ·+ +checking 1/3`, `◑ T2 +(✻ )?fixing ·+ +fixing 2/3`,
		`⇡ T3 +(✻ )?merging ·+ +merging`, `◌ T6 +(✻ )?queued ·+ +waiting T1,T2`, `● T5 +(✻ )?plain ·+ +working`} {
		if !regexp.MustCompile(want).MatchString(text) {
			t.Errorf("no row %q in\n%s", want, text)
		}
	}
	if !strings.HasPrefix(rows[0], "▎ ◆ T4") {
		t.Errorf("needs you first: %q", rows[0])
	}
}

// The status line does not advance; it spawns `csquad advance` when a task
// would move, unless an advance holds its lock.
func TestStatuslineSpawnsAdvance(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:true")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	statusline(t, h, sid, "CSQUAD_SETTLE_MS=600000")
	if exists(filepath.Join(h.Csquad, "logs", "advance.log")) {
		t.Fatal("spawned although nothing would move")
	}
	statusline(t, h, sid, "CSQUAD_SETTLE_MS=0")
	waitPhase(t, h, n, store.Ready)
	b, _ := os.ReadFile(filepath.Join(h.Csquad, "logs", "advance.log"))
	if !strings.Contains(string(b), "csquad advance\n") {
		t.Errorf("advance.log:\n%s", b)
	}
}
