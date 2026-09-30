package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/dispatch"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/queue"
)

// binaries holds the csquad and fake claude executables built once by TestMain.
var binaries struct{ csquad, claude string }

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "csquad-it-")
	if err != nil {
		panic(err)
	}
	binaries.csquad = filepath.Join(dir, "csquad")
	binaries.claude = filepath.Join(dir, "claude")
	for out, pkg := range map[string]string{binaries.csquad: "../../cmd/csquad", binaries.claude: "./testdata/fakeclaude"} {
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			panic(fmt.Sprintf("build %s: %v\n%s", pkg, err, b))
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// env is an isolated home, data, config and fake Claude Code for one test.
type env struct {
	t                                 *testing.T
	home, data, config, claude, state string
}

func newEnv(t *testing.T, slots int) *env {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	e := &env{t: t, home: filepath.Join(root, "home"), data: filepath.Join(root, "data"), config: filepath.Join(root, "config"),
		claude: filepath.Join(root, "claude"), state: filepath.Join(root, "fake")}
	for _, d := range []string{e.home, e.claude, e.state, filepath.Join(e.config, "csquad")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	e.write(filepath.Join(e.config, "csquad", "config.toml"), fmt.Sprintf("slots = %d\n", slots))
	e.write(filepath.Join(e.claude, ".claude.json"), "{}")
	t.Cleanup(e.stop)
	return e
}

func (e *env) write(path, content string) {
	e.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// dir creates a project directory under the test home.
func (e *env) dir(name string) string {
	e.t.Helper()
	d := filepath.Join(e.home, name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		e.t.Fatal(err)
	}
	return d
}

// run executes csquad in dir and returns its combined output and exit code.
func (e *env) run(dir, stdin string, args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(binaries.csquad, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + e.home, "XDG_DATA_HOME=" + e.data, "XDG_CONFIG_HOME=" + e.config,
		"XDG_STATE_HOME=" + filepath.Join(e.home, ".state"), "CLAUDE_CONFIG_DIR=" + e.claude, "CSQUAD_CLAUDE=" + binaries.claude,
		"FAKE_CLAUDE_STATE=" + e.state, "CSQUAD_DISPATCH_INTERVAL=30ms",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	return string(out), code
}

// ok runs csquad and fails the test on a non-zero exit.
func (e *env) ok(dir string, args ...string) string {
	e.t.Helper()
	out, code := e.run(dir, "", args...)
	if code != 0 {
		e.t.Fatalf("csquad %v exited %d:\n%s", args, code, out)
	}
	return out
}

type fakeSession struct {
	ID, SessionID, Name, Cwd, State string
}

// sessions returns the fake sessions keyed by name.
func (e *env) sessions() map[string]fakeSession {
	e.t.Helper()
	out := map[string]fakeSession{}
	files, _ := filepath.Glob(filepath.Join(e.state, "*.json"))
	for _, f := range files {
		var s fakeSession
		data, err := os.ReadFile(f)
		if err == nil && json.Unmarshal(data, &s) == nil {
			out[s.Name] = s
		}
	}
	return out
}

// setState changes a fake session's state, e.g. to done or blocked.
func (e *env) setState(s fakeSession, state string) {
	e.t.Helper()
	path := filepath.Join(e.state, s.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	raw["state"] = state
	data, _ = json.Marshal(raw)
	e.write(path, string(data))
}

func (e *env) waitFor(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *env) dataDir() string { return filepath.Join(e.data, "csquad") }

// stop cancels whatever is still queued and waits for the detached
// dispatcher to exit, so no process outlives the test.
func (e *env) stop() {
	if store, err := queue.Open(filepath.Join(e.dataDir(), "csquad.db")); err == nil {
		if queued, err := store.List(queue.Queued); err == nil {
			for _, t := range queued {
				_ = store.Cancel(t.ID)
			}
		}
		_ = store.Close()
	}
	_ = os.Remove(filepath.Join(e.state, "AGENTS_FAIL"))
	deadline := time.Now().Add(10 * time.Second)
	for dispatch.Running(e.dataDir()) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *env) peak() int {
	data, _ := os.ReadFile(filepath.Join(e.state, "peak"))
	n, _ := strconv.Atoi(string(data))
	return n
}

func TestConcurrentAddsRespectSlots(t *testing.T) {
	e := newEnv(t, 3)
	dir := e.dir("proj")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.ok(dir, "add", "--name", fmt.Sprint("task-", i), "--", fmt.Sprint("do ", i))
		}()
	}
	wg.Wait()
	e.waitFor("three sessions", func() bool { return len(e.sessions()) == 3 })
	time.Sleep(150 * time.Millisecond)
	if n := len(e.sessions()); n != 3 {
		t.Fatalf("launched %d sessions with 3 slots", n)
	}
	// Finish sessions one by one until every task has launched.
	for len(e.sessions()) < 20 {
		for _, s := range e.sessions() {
			if s.State == "working" {
				e.setState(s, "done")
				break
			}
		}
		before := len(e.sessions())
		e.waitFor("the next launch", func() bool { return len(e.sessions()) > before || len(e.sessions()) == 20 })
	}
	if p := e.peak(); p > 3 {
		t.Fatalf("peak of %d working sessions with 3 slots", p)
	}
	store, err := queue.Open(filepath.Join(e.dataDir(), "csquad.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	tasks, _ := store.List("")
	if len(tasks) != 20 || tasks[0].ID != 1 || tasks[19].ID != 20 {
		t.Fatalf("tasks: %d, want IDs 1..20", len(tasks))
	}
	e.waitFor("the dispatcher to exit", func() bool { return !dispatch.Running(e.dataDir()) })
	args, _ := os.ReadFile(filepath.Join(e.state, e.sessions()["task-0"].ID+".args"))
	if !strings.Contains(string(args), "--append-system-prompt\n# csquad worker") || !strings.HasSuffix(string(args), "--\ndo 0") {
		t.Fatalf("launch arguments:\n%s", args)
	}
}

func TestBlockedSessionHoldsItsSlot(t *testing.T) {
	e := newEnv(t, 1)
	dir := e.dir("proj")
	e.ok(dir, "add", "--name", "a", "first")
	e.ok(dir, "add", "--name", "b", "second")
	e.waitFor("a", func() bool { _, ok := e.sessions()["a"]; return ok })
	e.setState(e.sessions()["a"], "blocked")
	time.Sleep(200 * time.Millisecond)
	if _, ok := e.sessions()["b"]; ok {
		t.Fatal("launched past a session waiting for input")
	}
	if out := e.ok(dir, "ls"); !strings.Contains(out, "needs input") || !strings.Contains(out, "1/1 slots busy") {
		t.Fatalf("ls:\n%s", out)
	}
	e.setState(e.sessions()["a"], "done")
	e.waitFor("b", func() bool { _, ok := e.sessions()["b"]; return ok })
}

func TestLaunchFailureDoesNotStallTheQueue(t *testing.T) {
	e := newEnv(t, 1)
	dir := e.dir("proj")
	e.ok(dir, "add", "--name", "bad", "LAUNCH_FAIL please")
	e.ok(dir, "add", "--name", "good", "fine")
	e.waitFor("good", func() bool { _, ok := e.sessions()["good"]; return ok })
	out := e.ok(dir, "ls")
	if !strings.Contains(out, "T1  launch failed") || !strings.Contains(out, "Workspace not trusted") || !strings.Contains(out, "T2  working") {
		t.Fatalf("ls:\n%s", out)
	}
	if out := e.ok(dir, "peek", "T1"); !strings.Contains(out, "no session yet") || !strings.Contains(out, "Workspace not trusted") {
		t.Fatalf("peek:\n%s", out)
	}
	if out := e.ok(dir, "finish", "T1"); !strings.Contains(out, "never started") {
		t.Fatalf("finish:\n%s", out)
	}
	if out := e.ok(dir, "ls"); strings.Contains(out, "T1 ") {
		t.Fatalf("finished task still listed:\n%s", out)
	}
	if out := e.ok(dir, "ls", "--history"); !strings.Contains(out, "T1  finished") {
		t.Fatalf("ls --history:\n%s", out)
	}
}

func TestAddValidation(t *testing.T) {
	e := newEnv(t, 1)
	dir := e.dir("proj")
	for _, c := range []struct {
		name, stdin string
		args        []string
		code        int
		want        string
	}{
		{"empty", "", []string{"add", "  "}, 2, "empty"},
		{"args and file", "", []string{"add", "--file", "-", "text"}, 2, "not both"},
		{"missing cwd", "", []string{"add", "--cwd", filepath.Join(dir, "nope"), "x"}, 1, "task directory"},
		{"home", "", []string{"add", "--cwd", e.home, "x"}, 1, "home directory"},
		{"huge", "", []string{"add", strings.Repeat("x", 100<<10)}, 2, "limit is 96 KiB"},
		{"unknown command", "", []string{"lss"}, 2, "pass claude arguments after --"},
		{"bad id", "", []string{"peek", "abc"}, 2, "invalid task ID"},
		{"missing task", "", []string{"peek", "T99"}, 1, "csquad ls --all"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, code := e.run(dir, c.stdin, c.args...)
			if code != c.code || !strings.Contains(out, c.want) {
				t.Fatalf("exit %d, output:\n%s", code, out)
			}
		})
	}
	// A prompt read from stdin, starting with a dash and spanning lines.
	out, code := e.run(dir, "-starts with a dash\nsecond line 你好\n", "add", "--name", "stdin", "--file", "-")
	if code != 0 {
		t.Fatalf("stdin add: %d\n%s", code, out)
	}
	e.waitFor("stdin", func() bool { _, ok := e.sessions()["stdin"]; return ok })
	args, _ := os.ReadFile(filepath.Join(e.state, e.sessions()["stdin"].ID+".args"))
	if !strings.HasSuffix(string(args), "\n--\n-starts with a dash\nsecond line 你好") {
		t.Fatalf("prompt not passed verbatim after --:\n%s", args)
	}
	if out := e.ok(dir, "peek", "T1"); !strings.Contains(out, "-starts with a dash") {
		t.Fatalf("peek:\n%s", out)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t, 1)
	dir := e.dir("proj")
	e.ok(dir, "add", "--name", "a", "first")
	e.waitFor("a", func() bool { _, ok := e.sessions()["a"]; return ok })
	e.ok(dir, "add", "--name", "b", "second")
	if out := e.ok(dir, "cancel", "T2"); !strings.Contains(out, "T2 cancelled") {
		t.Fatal(out)
	}
	for args, want := range map[string]string{"T2": "cancelled, not queued", "T1": "claude stop", "T9": "not found"} {
		if out, code := e.run(dir, "", "cancel", args); code != 1 || !strings.Contains(out, want) {
			t.Fatalf("cancel %s: %d\n%s", args, code, out)
		}
	}
	if out := e.ok(dir, "peek", "T2"); !strings.Contains(out, "cancelled") {
		t.Fatal(out)
	}
	e.setState(e.sessions()["a"], "done")
	e.waitFor("the dispatcher to exit", func() bool { return !dispatch.Running(e.dataDir()) })
	if _, ok := e.sessions()["b"]; ok {
		t.Fatal("launched a cancelled task")
	}
}

func TestLsScopes(t *testing.T) {
	e := newEnv(t, 5)
	a, sub, b := e.dir("a"), e.dir("a/sub"), e.dir("b")
	e.ok(a, "add", "--name", "in-a", "x")
	e.ok(sub, "add", "--name", "in-sub", "x")
	e.ok(b, "add", "--name", "in-b", "x")
	e.waitFor("three sessions", func() bool { return len(e.sessions()) == 3 })
	check := func(dir string, args []string, want, not []string) {
		t.Helper()
		out := e.ok(dir, append([]string{"ls"}, args...)...)
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Fatalf("ls %v in %s lacks %q:\n%s", args, dir, w, out)
			}
		}
		for _, n := range not {
			if strings.Contains(out, n) {
				t.Fatalf("ls %v in %s shows %q:\n%s", args, dir, n, out)
			}
		}
	}
	check(a, nil, []string{"in-a", "in-sub"}, []string{"in-b", "DIR"})
	check(sub, nil, []string{"in-sub"}, []string{"in-a", "in-b"})
	check(sub, []string{"--all"}, []string{"in-a", "in-sub", "in-b", "DIR", "~/b"}, nil)
	check(sub, nil, []string{"Working on in-sub"}, nil)
}

func TestSessionStateUnavailable(t *testing.T) {
	e := newEnv(t, 1)
	dir := e.dir("proj")
	e.write(filepath.Join(e.state, "AGENTS_FAIL"), "")
	e.ok(dir, "add", "--name", "a", "first")
	out := e.ok(dir, "ls")
	if !strings.Contains(out, "session state unavailable") || !strings.Contains(out, "?/1 slots busy") || !strings.Contains(out, "T1  queued") {
		t.Fatalf("ls:\n%s", out)
	}
	if _, ok := e.sessions()["a"]; ok {
		t.Fatal("launched without knowing the slot usage")
	}
	// Once the listing works again, the still running dispatcher proceeds.
	_ = os.Remove(filepath.Join(e.state, "AGENTS_FAIL"))
	e.waitFor("a", func() bool { _, ok := e.sessions()["a"]; return ok })
}

func TestFinishDiscardsOnlyVerifiedWork(t *testing.T) {
	e := newEnv(t, 5)
	plain := e.dir("plain")
	repo := e.dir("repo")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	e.ok(plain, "add", "--name", "plain", "x")
	e.ok(repo, "add", "--name", "repo", "x")
	e.waitFor("two sessions", func() bool { return len(e.sessions()) == 2 })
	for _, s := range e.sessions() {
		e.write(filepath.Join(e.state, s.ID+".unpushed"), "")
	}
	if out, code := e.run(plain, "", "finish", "T1"); code != 1 || !strings.Contains(out, "still working") {
		t.Fatalf("finish while working: %d\n%s", code, out)
	}
	e.setState(e.sessions()["plain"], "done")
	e.setState(e.sessions()["repo"], "done")
	// Outside git nothing verifies the session's commits: claude rm must refuse.
	out, code := e.run(plain, "", "finish", "T1")
	if code != 1 || !strings.Contains(out, "refused") {
		t.Fatalf("finish outside git: %d\n%s", code, out)
	}
	if _, ok := e.sessions()["plain"]; !ok {
		t.Fatal("removed a session with unverified unpushed commits")
	}
	// In git, with the work on main, the discard is confirmed.
	out = e.ok(repo, "finish", "T2")
	if !strings.Contains(out, "T2 finished") {
		t.Fatal(out)
	}
	if _, ok := e.sessions()["repo"]; ok {
		t.Fatal("session kept after finish")
	}
	if out := e.ok(repo, "finish", "T2"); !strings.Contains(out, "already finished") {
		t.Fatal(out)
	}
}

func TestTrust(t *testing.T) {
	e := newEnv(t, 5)
	dir := e.dir("proj")
	e.ok(dir, "add", "--name", "a", "x")
	e.waitFor("a", func() bool { _, ok := e.sessions()["a"]; return ok })
	data, _ := os.ReadFile(filepath.Join(e.claude, ".claude.json"))
	if !strings.Contains(string(data), `"`+dir+`"`) || !strings.Contains(string(data), `"hasTrustDialogAccepted": true`) {
		t.Fatalf(".claude.json:\n%s", data)
	}
	// A corrupt state file fails the launch and is left exactly as it was.
	corrupt := "{not json"
	e.write(filepath.Join(e.claude, ".claude.json"), corrupt)
	other := e.dir("other")
	e.ok(other, "add", "--name", "b", "x")
	// add returns before the detached dispatcher takes its lock, so wait for
	// the task itself rather than for the dispatcher to be gone.
	e.waitFor("the launch attempt", func() bool { return !strings.Contains(e.ok(other, "ls"), "T2  queued") })
	if out := e.ok(other, "ls"); !strings.Contains(out, "launch failed") || !strings.Contains(out, "parse") {
		t.Fatalf("ls:\n%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(e.claude, ".claude.json")); string(data) != corrupt {
		t.Fatalf("corrupt state rewritten: %s", data)
	}
}

func TestSecondDispatcherExits(t *testing.T) {
	e := newEnv(t, 1)
	release, err := filelock.Acquire(e.dataDir(), "dispatcher", true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if out := e.ok(e.dir("proj"), "dispatch"); !strings.Contains(out, "already running") {
		t.Fatal(out)
	}
}
