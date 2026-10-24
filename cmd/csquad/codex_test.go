package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/engine/codex/codextest"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

var launchedCodexRe = regexp.MustCompile(`^launched T(\d+) · (.*) · session ([0-9a-f-]{36}) · `)

// codexHome makes a fake running Codex daemon for h and a repository.
func codexHome(t *testing.T) (*testutil.Home, *codextest.Server, string) {
	h := testutil.NewHome(t)
	srv := codextest.New(t, filepath.Join(h.Home, ".codex"), true)
	codextest.LinkFake(t, h.Bin)
	h.ExtraEnv = append(append([]string{}, gitIdentity...), srv.Env()...)
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(repo, 0o755)
	git(t, h, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	git(t, h, repo, "add", ".")
	git(t, h, repo, "commit", "-qm", "init")
	return h, srv, repo
}

const parentThread = "019a1111-2222-7333-8444-555566667777"

func fromCodex(extra ...string) []string {
	return append([]string{"CODEX_THREAD_ID=" + parentThread}, extra...)
}

func TestCodexTaskFromLaunchToFinish(t *testing.T) {
	h, srv, repo := codexHome(t)
	// A Claude task first: numbering runs across engines.
	ok(t, csq(t, h, opts{stdin: "claude work"}, "launch", "--title", "claude job", "--cwd", repo))
	r := ok(t, csq(t, h, opts{stdin: "codex work\n", env: fromCodex()}, "launch", "--title", "codex job", "--engine", "codex", "--cwd", repo, "--", "-m", "gpt-x", "-c", "k=1"))
	m := launchedCodexRe.FindStringSubmatch(r.stdout)
	if m == nil || m[1] != "2" {
		t.Fatalf("%q", r.stdout)
	}
	id := m[3]
	start := srv.Last("thread/start")
	rules, _ := start["developerInstructions"].(string)
	if start["cwd"] != repo || start["model"] != "gpt-x" || start["sandbox"] != "danger-full-access" || start["approvalPolicy"] != "never" ||
		!strings.Contains(rules, "-C REPO worktree add ../NAME-wt/t12-") || !strings.HasSuffix(rules, "Your task ID is T2.\n") || strings.Contains(rules, "Claude Code") {
		t.Errorf("thread/start %v", start)
	}
	if th := srv.Thread(id); th == nil || th.Name != "T2 · codex job" {
		t.Fatalf("thread %+v", th)
	}

	// The dispatching Codex thread sees its own task only.
	r = ok(t, csq(t, h, opts{env: fromCodex()}, "status", "--json"))
	var list []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["id"] != "T2" || list[0]["engine"] != "codex" || list[0]["state"] != "working" ||
		list[0]["parentSession"] != "codex:"+parentThread || list[0]["attach"] != "codex resume "+id {
		t.Errorf("status %s", r.stdout)
	}
	if list[0]["session"] != id {
		t.Errorf("session %v", list[0]["session"])
	}
	// Outside a session every task shows.
	if r := ok(t, csq(t, h, opts{}, "status")); !strings.Contains(r.stdout, "## T1 · claude job") || !strings.Contains(r.stdout, "## T2 · codex job") {
		t.Errorf("status %s", r.stdout)
	}

	r = ok(t, csq(t, h, opts{env: fromCodex()}, "send", "T2", "also the docs"))
	if !strings.Contains(r.stdout, "sent to T2 · codex job: added to the running turn") {
		t.Errorf("send %q", r.stdout)
	}

	// The worker makes its worktree as its rules say, commits, and ends.
	wt := filepath.Join(h.Root, "repo-wt", "t2-codex-job")
	git(t, h, repo, "worktree", "add", "-q", "../repo-wt/t2-codex-job", "-b", "task/t2-codex-job")
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	git(t, h, wt, "add", ".")
	git(t, h, wt, "commit", "-qm", "codex fix")
	srv.Complete(id, "Committed b.txt on task/t2-codex-job.")
	r = ok(t, csq(t, h, opts{env: fromCodex()}, "peek", "T2"))
	if !strings.Contains(r.stdout, "T2 · codex job · idle") || !strings.Contains(r.stdout, "[user]\ncodex work") ||
		!strings.Contains(r.stdout, "[tools] commandExecution ×1") || !strings.Contains(r.stdout, "[assistant]\nCommitted b.txt") {
		t.Errorf("peek %s", r.stdout)
	}

	// finish finds the worktree by the t2- naming, refuses while unmerged.
	r = csq(t, h, opts{env: fromCodex()}, "finish", "T2")
	if r.code != 1 || !strings.Contains(r.stderr, "branch task/t2-codex-job is not merged into main") {
		t.Fatalf("%+v", r)
	}
	git(t, h, repo, "merge", "-q", "--ff-only", "task/t2-codex-job")
	r = ok(t, csq(t, h, opts{env: fromCodex()}, "finish", "T2"))
	if !strings.Contains(r.stdout, "merged into main: task/t2-codex-job · removed worktree "+wt+", branch task/t2-codex-job, archived thread "+id) {
		t.Errorf("finish %s", r.stdout)
	}
	if exists(wt) || !srv.Thread(id).Archived {
		t.Error("worktree or thread left")
	}
}

func TestCodexLaunchValidatesThreadOptionsFirst(t *testing.T) {
	h, srv, repo := codexHome(t)
	for _, bad := range [][]string{{"--yolo"}, {"-s", "everything"}, {"-c", "novalue"}} {
		r := csq(t, h, opts{stdin: "x"}, append([]string{"launch", "--title", "t", "--engine", "codex", "--cwd", repo, "--"}, bad...)...)
		if r.code == 0 || !strings.Contains(r.stderr, bad[len(bad)-1]) {
			t.Errorf("%v: %+v", bad, r)
		}
	}
	// Refused before the prompt is even read.
	if r := csq(t, h, opts{}, "launch", "--title", "t", "--engine", "codex", "--cwd", repo, "--", "--yolo"); r.code == 0 || !strings.Contains(r.stderr, "--yolo is not a Codex thread option") {
		t.Errorf("%+v", r)
	}
	if len(srv.Calls()) != 0 {
		t.Errorf("the daemon was asked: %v", srv.Methods())
	}
	// No row was written: the next task is T1.
	r := ok(t, csq(t, h, opts{stdin: "x"}, "launch", "--title", "t", "--engine", "codex", "--cwd", repo))
	if m := launchedCodexRe.FindStringSubmatch(r.stdout); m == nil || m[1] != "1" {
		t.Errorf("%q", r.stdout)
	}
	// A failed start leaves no row either.
	srv.Fail("thread/start", "boom")
	if r := csq(t, h, opts{stdin: "x"}, "launch", "--title", "t", "--engine", "codex", "--cwd", repo); r.code == 0 || !strings.Contains(r.stderr, "boom") {
		t.Errorf("%+v", r)
	}
	srv.Fail("thread/start", "")
	r = ok(t, csq(t, h, opts{stdin: "x"}, "launch", "--title", "t", "--engine", "codex", "--cwd", repo))
	if m := launchedCodexRe.FindStringSubmatch(r.stdout); m == nil || m[1] != "2" {
		t.Errorf("%q", r.stdout)
	}
}

// The status line never starts the Codex daemon.
func TestStatuslineDoesNotStartTheCodexDaemon(t *testing.T) {
	h, srv, repo := codexHome(t)
	launched := ok(t, csq(t, h, opts{stdin: "x", env: mine()}, "launch", "--title", "cx", "--engine", "codex", "--cwd", repo)).stdout
	if !strings.HasPrefix(launched, "launched T1") {
		t.Fatal(launched)
	}
	os.Remove(srv.Link) // the daemon went away
	r := ok(t, csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100"}}, "statusline"))
	if srv.DaemonStarts() != 0 {
		t.Errorf("the status line started the daemon: %q", r.stdout)
	}
	// A command does start it.
	ok(t, csq(t, h, opts{env: mine()}, "status"))
	if srv.DaemonStarts() != 1 {
		t.Errorf("status did not start the daemon")
	}
}

// One engine failing hides none of another engine's tasks: status and the
// status line show the Codex task while claude agents fails, wait reports
// it, and wait fails only for a task of the failing engine.
func TestEnginesApart(t *testing.T) {
	h, srv, repo := codexHome(t)
	ok(t, csq(t, h, opts{stdin: "x", env: mine()}, "launch", "--title", "claude job", "--cwd", repo))
	r := ok(t, csq(t, h, opts{stdin: "x", env: mine()}, "launch", "--title", "codex job", "--engine", "codex", "--cwd", repo))
	id := launchedCodexRe.FindStringSubmatch(r.stdout)[3]
	broken := mine("FAKE_AGENTS_FAIL=1")

	r = ok(t, csq(t, h, opts{env: broken}, "status"))
	if !strings.Contains(r.stdout, "## T2 · codex job\nstate: working") || !strings.Contains(r.stderr, "claude agents failed") {
		t.Errorf("status %+v", r)
	}
	lines := statusline(t, h, sid, "FAKE_AGENTS_FAIL=1")
	if text := strings.Join(lines, "\n"); !strings.Contains(text, "T2") || !strings.Contains(text, "codex job") || !strings.Contains(text, "working") {
		t.Errorf("status line %q", lines)
	}

	srv.Complete(id, "done.")
	r = waitFor(t, h, append([]string{"CSQUAD_WAIT_FAIL_MS=60000"}, broken...), nil, "T2")
	if r.code != 0 || r.stdout != "T2 · codex job · already idle\n" {
		t.Errorf("wait for the codex task: %+v", r)
	}
	r = waitFor(t, h, append([]string{"CSQUAD_WAIT_FAIL_MS=300"}, broken...), nil, "T1")
	if r.code != 1 || !strings.Contains(r.stderr, "the claude engine keeps failing") {
		t.Errorf("wait for the claude task: %+v", r)
	}
}
