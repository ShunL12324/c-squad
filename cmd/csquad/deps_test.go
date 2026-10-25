package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/store"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

// ---- implicit merges for the tasks queued after a task -------------------------

// queueAfter launches a task --after others from the main session and
// returns launch's output.
func queueAfter(t *testing.T, h *testutil.Home, repo, title, after string, extra ...string) string {
	t.Helper()
	return ok(t, csq(t, h, opts{stdin: "build on it", env: mine(), dir: repo},
		append([]string{"launch", "--title", title, "--cwd", repo, "--after", after}, extra...)...)).stdout
}

// promptOf is the prompt a task's session started with.
func promptOf(h *testutil.Home, name string) string {
	for _, a := range h.Argv() {
		if testutil.Arg(a, "--name") == name {
			return a[len(a)-1]
		}
	}
	return ""
}

// entered moves the time a task entered its phase back by d, as if it had
// been there that long.
func entered(t *testing.T, h *testutil.Home, id int, d time.Duration) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(h.Csquad, "csquad.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET updated_at = updated_at - ? WHERE id = ?`, d.Milliseconds(), id); err != nil {
		t.Fatal(err)
	}
}

// T2 --after T1, neither with --merge: when T1's worker stops, csquad merges
// T1 into the main checkout's branch and T2 starts from it.
func TestImplicitMergeWhenTheWorkerStops(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first")
	out := queueAfter(t, h, repo, "second", "T1")
	if !strings.Contains(out, "\n  T1 merges into main when it is done, then T2 starts") {
		t.Errorf("launch: %s", out)
	}
	if r := ok(t, csq(t, h, opts{env: mine()}, "status")); !strings.Contains(r.stdout, "flow: merge into main, then T2 starts") {
		t.Errorf("status:\n%s", r.stdout)
	}
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	writeTranscript(t, h, "1", `{"type":"assistant","message":{"content":[{"type":"text","text":"Added b.txt."}]}}`)
	advance(t, h)
	if task := getTask(t, h, t1); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	if git(t, h, repo, "show", "main:b.txt") != "b" || strings.Contains(branches(t, h, repo), "worktree-t1-fix") {
		t.Errorf("not merged and cleaned up: %s", branches(t, h, repo))
	}
	waitPhase(t, h, 2, store.Running)
	if p := promptOf(h, "T2 · second"); !strings.Contains(p, "T1 · first: merged worktree-t1-fix into main.") || !strings.Contains(p, "Added b.txt.") {
		t.Errorf("prompt %q", p)
	}
	want := "T1 running→merging,T1 merging→done,T2 waiting→running"
	if got := strings.Join(events(h), ","); got != want {
		t.Errorf("events %s", got)
	}
	// A task nothing waits for stays as before: running until finish.
	t3 := launchIn(t, h, repo, "third")
	work(t, h, repo, t3, map[string]string{"c.txt": "c\n"})
	advance(t, h)
	if task := getTask(t, h, t3); task.Phase != store.Running {
		t.Errorf("%s", brief(task))
	}
}

// The case of a 3.0 chain: T1's worker stopped long ago (phase running,
// settled) when T2 was queued after it. The status line sees it and the
// advance it spawns merges T1, with the default settle time.
func TestDependentQueuedAfterTheWorkerStopped(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first")
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	setState(h, "1", func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	entered(t, h, t1, 3*time.Hour)
	advance(t, h)
	if p := getTask(t, h, t1).Phase; p != store.Running {
		t.Fatalf("merged although nothing waits for it: %s", p)
	}
	// Queued directly in the table, as an older csquad left it.
	db := openDB(t, h)
	db.Insert(t.Context(), store.Task{Title: "second", Cwd: repo, Engine: "claude", Prompt: "next", Phase: store.Waiting, After: []int{t1}, ParentSession: "claude:" + sid})
	db.Close()
	if rows := strings.Join(statusline(t, h, sid), "\n"); !regexp.MustCompile(`✓ T1 (✻ )?first[ ·]+done`).MatchString(rows) {
		t.Errorf("ledger:\n%s", rows)
	}
	waitPhase(t, h, t1, store.Done)
	waitPhase(t, h, 2, store.Running)
	if git(t, h, repo, "show", "main:b.txt") != "b" {
		t.Error("main lacks T1's work")
	}
	b, _ := os.ReadFile(filepath.Join(h.Csquad, "logs", "advance.log"))
	if !strings.Contains(string(b), "csquad advance\n") {
		t.Errorf("advance.log:\n%s", b)
	}
}

// With a check, the merge comes after the pass: the check's own process
// lands it.
func TestImplicitMergeAfterTheCheckPasses(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first", "--check", "cmd:test -f b.txt")
	queueAfter(t, h, repo, "second", "T1")
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	waitPhase(t, h, t1, store.Done)
	waitPhase(t, h, 2, store.Running)
	if git(t, h, repo, "show", "main:b.txt") != "b" {
		t.Error("main lacks T1's work")
	}
	if got := strings.Join(events(h)[:3], ","); got != "T1 running→checking,T1 checking→merging,T1 merging→done" {
		t.Errorf("events %v", events(h))
	}

	// A task that was ready before a task was queued after it (a check
	// passed, no merge) merges then.
	t3 := launchIn(t, h, repo, "third", "--check", "cmd:true")
	work(t, h, repo, t3, map[string]string{"c.txt": "c\n"})
	advance(t, h)
	waitPhase(t, h, t3, store.Ready)
	queueAfter(t, h, repo, "fourth", fmt.Sprintf("T%d", t3))
	advance(t, h)
	waitPhase(t, h, t3, store.Done)
	waitPhase(t, h, 4, store.Running)
	if git(t, h, repo, "show", "main:c.txt") != "c" {
		t.Error("main lacks T3's work")
	}
}

// --no-merge: the task is never merged for its dependents; the status line
// and status say what blocks them, and finish releases them.
func TestNoMergeKeepsTheDependentsWaiting(t *testing.T) {
	h, repo := flowRepo(t)
	r := ok(t, csq(t, h, opts{stdin: "do it", env: mine(), dir: repo}, "launch", "--title", "first", "--cwd", repo, "--no-merge"))
	if !strings.Contains(r.stdout, "then: your review (--no-merge): csquad does not merge it") {
		t.Errorf("launch: %s", r.stdout)
	}
	if task := getTask(t, h, 1); !task.Hold || task.MergeInto != "" {
		t.Fatalf("%+v", task)
	}
	out := queueAfter(t, h, repo, "second", "T1")
	if !strings.Contains(out, "\n  T1 is held for your review: csquad release T1 merges it, or merge it and csquad finish T1, then T2 starts") {
		t.Errorf("launch: %s", out)
	}
	rows := strings.Join(statusline(t, h, sid, "COLUMNS=160"), "\n")
	if !regexp.MustCompile(`◌ T2 (✻ )?second[ ·]+waiting T1 · review`).MatchString(rows) || strings.Contains(rows, "finish to start") {
		t.Errorf("ledger while T1 works:\n%s", rows)
	}
	wt := work(t, h, repo, 1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	if p := getTask(t, h, 1).Phase; p != store.Running {
		t.Fatalf("a held task moved: %s", p)
	}
	if p := getTask(t, h, 2).Phase; p != store.Waiting {
		t.Fatalf("T2: %s", p)
	}
	raw := csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=160"}}, "statusline").stdout
	if !regexp.MustCompile(`\x1b\[1;38;2;46;166;98mdone · finish to start T2\x1b\[0m`).MatchString(raw) {
		t.Errorf("the held row is not bold green: %q", raw)
	}
	rows = strings.Join(statusline(t, h, sid, "COLUMNS=160"), "\n")
	if !regexp.MustCompile(`✓ T1 (✻ )?first[ ·]+done · finish to start T2`).MatchString(rows) || !regexp.MustCompile(`◌ T2 (✻ )?second[ ·]+waiting T1 · review`).MatchString(rows) {
		t.Errorf("ledger:\n%s", rows)
	}
	if !strings.Contains(strings.Split(rows, "\n")[0], "T1") {
		t.Errorf("the held row comes first:\n%s", rows)
	}
	st := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout
	for _, want := range []string{"## T1 · first\nstate: done · finish to start T2 ·",
		"next: review its work; csquad release T1 merges it and starts T2 (or merge it yourself, then csquad finish T1)",
		"state: waiting T1 · review ·", "next: T1 waits for your review: csquad release T1 (or finish it) starts this task"} {
		if !strings.Contains(st, want) {
			t.Errorf("status lacks %q:\n%s", want, st)
		}
	}
	var list []map[string]any
	json.Unmarshal([]byte(ok(t, csq(t, h, opts{env: mine()}, "status", "--json")).stdout), &list)
	if len(list) != 2 || list[0]["id"] != "T1" || list[0]["hold"] != true || list[0]["state"] != "done · finish to start T2" || !strings.HasPrefix(fmt.Sprint(list[0]["next"]), "review its work") {
		t.Errorf("status --json %v", list)
	}
	// The user reviews, merges and finishes it: T2 starts.
	git(t, h, repo, "merge", "-q", "--ff-only", "worktree-t1-fix")
	ok(t, csq(t, h, opts{env: mine()}, "finish", "T1"))
	if exists(wt) || getTask(t, h, 2).Phase != store.Running {
		t.Errorf("finish: %s", brief(getTask(t, h, 2)))
	}
	if r := csq(t, h, opts{stdin: "x", dir: repo}, "launch", "--title", "x", "--cwd", repo, "--merge", "--no-merge"); r.code != 1 || !strings.Contains(r.stderr, "--merge and --no-merge exclude each other") {
		t.Errorf("%+v", r)
	}
}

// hold and release on a task that exists: the user asks to review it after
// all, then lets csquad merge it.
func TestHoldAndReleaseAnExistingTask(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first")
	queueAfter(t, h, repo, "second", "T1")
	r := ok(t, csq(t, h, opts{env: mine()}, "hold", "T1"))
	if r.stdout != "held T1 · first for your review: csquad does not merge it; T2 waits until you finish or release it\n" {
		t.Errorf("hold: %q", r.stdout)
	}
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	if task := getTask(t, h, t1); task.Phase != store.Running || !task.Hold {
		t.Fatalf("%s", brief(task))
	}
	r = ok(t, csq(t, h, opts{env: mine("CSQUAD_SETTLE_MS=0")}, "release", "T1"))
	if !strings.HasPrefix(r.stdout, "released T1 · first: csquad merges it when its work is done, then T2 starts\n") {
		t.Errorf("release: %q", r.stdout)
	}
	if task := getTask(t, h, t1); task.Phase != store.Done || task.Hold {
		t.Fatalf("%s", brief(task))
	}
	waitPhase(t, h, 2, store.Running)
	if git(t, h, repo, "show", "main:b.txt") != "b" {
		t.Error("main lacks T1's work")
	}
	// What hold refuses.
	if r := csq(t, h, opts{env: mine()}, "hold", "T1"); r.code != 1 || !strings.Contains(r.stderr, "T1 is already done") {
		t.Errorf("%+v", r)
	}
	launchIn(t, h, repo, "merged", "--merge")
	if r := csq(t, h, opts{env: mine()}, "hold", "T3"); r.code != 1 || !strings.Contains(r.stderr, "T3 merges into main by itself (launched with --merge)") {
		t.Errorf("%+v", r)
	}
	launchAs(t, h, "11111111-2222-4333-8444-555555555555", "theirs")
	if r := csq(t, h, opts{env: mine()}, "hold", "T4"); r.code != 1 || !strings.Contains(r.stderr, "pass --all to hold it anyway") {
		t.Errorf("%+v", r)
	}
}

// A conflict: the worker is asked to rebase as for --merge; when that does
// not resolve it the task needs you and its dependent keeps waiting.
func TestImplicitMergeConflictNeedsYou(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first")
	queueAfter(t, h, repo, "second", "T1")
	work(t, h, repo, t1, map[string]string{"a.txt": "task\n"})
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("main\n"), 0o644)
	git(t, h, repo, "commit", "-qam", "main moved")
	advance(t, h)
	if task := getTask(t, h, t1); task.Phase != store.Merging || task.Step != "asked" {
		t.Fatalf("%s", brief(task))
	}
	if msgs := sent(h); len(msgs) != 1 || !strings.Contains(msgs[0], "does not rebase cleanly onto main") {
		t.Errorf("sent %q", msgs)
	}
	reply(t, h, t1, "I could not.")
	advance(t, h)
	task := getTask(t, h, t1)
	if task.Phase != store.NeedsYou || task.Step != "merge" || !strings.HasPrefix(task.Note, "merge conflict: worktree-t1-fix still does not rebase onto main") {
		t.Errorf("%s", brief(task))
	}
	if p := getTask(t, h, 2).Phase; p != store.Waiting {
		t.Errorf("T2: %s", p)
	}
	rows := strings.Join(statusline(t, h, sid, "COLUMNS=160"), "\n")
	if !regexp.MustCompile(`◆ T1 +(✻ )?first[ ·]+needs you · merge conflict`).MatchString(rows) || !regexp.MustCompile(`◌ T2 (✻ )?second[ ·]+waiting T1\b`).MatchString(rows) {
		t.Errorf("ledger:\n%s", rows)
	}
	// The user resolves it; retry lands the merge and T2 starts.
	wt := filepath.Join(repo, ".claude", "worktrees", "t1-fix")
	gitNoFail(h, wt, "rebase", "main")
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("main\ntask\n"), 0o644)
	git(t, h, wt, "add", "a.txt")
	git(t, h, wt, "-c", "core.editor=true", "rebase", "--continue")
	ok(t, csq(t, h, opts{env: mine()}, "retry", "T1"))
	if task := getTask(t, h, t1); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	waitPhase(t, h, 2, store.Running)
}

// Several dependencies: the dependent starts when the last one merges; one
// task merges once for two dependents.
func TestSeveralDependencies(t *testing.T) {
	h, repo := flowRepo(t)
	t1 := launchIn(t, h, repo, "first")
	t2 := launchIn(t, h, repo, "second")
	out := queueAfter(t, h, repo, "third", "T1,T2")
	if !strings.Contains(out, "\n  T1 merges into main when it is done\n  T2 merges into main when it is done\n  then T3 starts") {
		t.Errorf("launch: %s", out)
	}
	queueAfter(t, h, repo, "fourth", "T1")
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	if p := getTask(t, h, t1).Phase; p != store.Done {
		t.Fatal(p)
	}
	waitPhase(t, h, 4, store.Running)
	if p := getTask(t, h, 3).Phase; p != store.Waiting {
		t.Fatalf("T3 started before T2 merged: %s", p)
	}
	work(t, h, repo, t2, map[string]string{"c.txt": "c\n"})
	advance(t, h)
	waitPhase(t, h, 3, store.Running)
	if git(t, h, repo, "show", "main:b.txt") != "b" || git(t, h, repo, "show", "main:c.txt") != "c" {
		t.Error("main lacks a dependency's work")
	}
	if p := promptOf(h, "T3 · third"); !strings.Contains(p, "after T1, T2, which are done") || !strings.Contains(p, "T2 · second: merged worktree-t2-fix into main.") {
		t.Errorf("prompt %q", p)
	}
}

// A task in place has nothing to merge: it is done when its worker stops,
// and its dependent starts. Its checkout is not touched.
func TestInPlaceDependencyIsDoneWhenItsWorkerStops(t *testing.T) {
	h, repo := flowRepo(t)
	launchIn(t, h, repo, "first", "--isolation", "none")
	out := queueAfter(t, h, repo, "second", "T1")
	if !strings.Contains(out, "\n  T1 works in place: it is done when its worker stops, then T2 starts") {
		t.Errorf("launch: %s", out)
	}
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("edited in place\n"), 0o644)
	setState(h, "1", func(s *testutil.Session) { s.State, s.Status = "working", "idle" })
	writeTranscript(t, h, "1", `{"type":"assistant","message":{"content":[{"type":"text","text":"Edited a.txt."}]}}`)
	advance(t, h)
	if task := getTask(t, h, 1); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	waitPhase(t, h, 2, store.Running)
	if p := promptOf(h, "T2 · second"); !strings.Contains(p, "T1 · first: finished. Its final message:\n<<<\nEdited a.txt.") {
		t.Errorf("prompt %q", p)
	}
	if git(t, h, repo, "status", "--porcelain") != "M a.txt" || git(t, h, repo, "log", "--format=%s", "-1") != "init" {
		t.Error("the checkout was touched")
	}
	for _, s := range h.Sessions() {
		if strings.HasPrefix(s.Name, "T1 ·") {
			t.Errorf("T1's session is left: %+v", s)
		}
	}
	if got := strings.Join(events(h), ","); got != "T1 running→merging,T1 merging→done,T2 waiting→running" {
		t.Errorf("events %s", got)
	}
}

// --merge BRANCH keeps its target with a task queued after it.
func TestExplicitMergeTargetWithADependent(t *testing.T) {
	h, repo := flowRepo(t)
	git(t, h, repo, "branch", "release")
	t1 := launchIn(t, h, repo, "first", "--merge", "release")
	out := queueAfter(t, h, repo, "second", "T1")
	if !strings.Contains(out, "\n  T1 merges into release when it is done (--merge), then T2 starts") {
		t.Errorf("launch: %s", out)
	}
	work(t, h, repo, t1, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	if task := getTask(t, h, t1); task.Phase != store.Done {
		t.Fatalf("%s", brief(task))
	}
	if git(t, h, repo, "log", "--format=%s", "-1", "release") != "work" || git(t, h, repo, "log", "--format=%s", "-1", "main") != "init" {
		t.Error("release not fast-forwarded, or main moved")
	}
	waitPhase(t, h, 2, store.Running)
	if p := promptOf(h, "T2 · second"); !strings.Contains(p, "merged worktree-t1-fix into release.") {
		t.Errorf("prompt %q", p)
	}
}

// A Codex task with a Claude task queued after it: its Stop hook spawns the
// advance, which merges its branch.
func TestCodexDependencyMerges(t *testing.T) {
	h, srv, repo := codexHome(t)
	r := ok(t, csq(t, h, opts{stdin: "codex work", env: mine()}, "launch", "--title", "codex job", "--engine", "codex", "--cwd", repo))
	id := launchedCodexRe.FindStringSubmatch(r.stdout)[3]
	queueAfter(t, h, repo, "second", "T1")
	wt := filepath.Join(h.Root, "repo-wt", "t1-codex-job")
	git(t, h, repo, "worktree", "add", "-q", "../repo-wt/t1-codex-job", "-b", "task/t1-codex-job")
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	git(t, h, wt, "add", ".")
	git(t, h, wt, "commit", "-qm", "codex fix")
	srv.Complete(id, "Committed b.txt on task/t1-codex-job.")
	ok(t, csq(t, h, opts{stdin: `{"session_id":"` + id + `"}`, env: []string{"CSQUAD_STOP_WAIT_MS=0"}}, "hook", "codex-stop"))
	waitPhase(t, h, 1, store.Done)
	waitPhase(t, h, 2, store.Running)
	b, _ := os.ReadFile(filepath.Join(h.Csquad, "logs", "advance.log"))
	if !strings.Contains(string(b), "csquad advance --stopped T1") {
		t.Errorf("advance.log:\n%s", b)
	}
	if git(t, h, repo, "show", "main:b.txt") != "b" || exists(wt) || !srv.Thread(id).Archived {
		t.Error("not merged, or the worktree or the thread is left")
	}
	if p := promptOf(h, "T2 · second"); !strings.Contains(p, "T1 · codex job: merged task/t1-codex-job into main.") || !strings.Contains(p, "Committed b.txt") {
		t.Errorf("prompt %q", p)
	}
}
