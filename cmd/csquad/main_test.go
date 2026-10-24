package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

// The test binary is csquad (CSQUAD_TEST_MAIN=1) and the fake claude (when
// started through its `claude` link).
func TestMain(m *testing.M) {
	testutil.MaybeFakeClaude()
	if os.Getenv("CSQUAD_TEST_MAIN") == "1" {
		os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

type result struct {
	code           int
	stdout, stderr string
}

type opts struct {
	stdin string
	env   []string
	dir   string
}

func csq(t *testing.T, h *testutil.Home, o opts, args ...string) result {
	t.Helper()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, args...)
	cmd.Env = h.Env(append([]string{"CSQUAD_TEST_MAIN=1"}, o.env...)...)
	cmd.Dir = h.Root
	if o.dir != "" {
		cmd.Dir = o.dir
	}
	cmd.Stdin = strings.NewReader(o.stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, out.String(), errb.String()}
}

func ok(t *testing.T, r result) result {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	return r
}

func launch(t *testing.T, h *testutil.Home, title string, extra ...string) result {
	t.Helper()
	return csq(t, h, opts{stdin: "do it\n"}, append([]string{"launch", "--title", title, "--cwd", h.Root}, extra...)...)
}

var launchedRe = regexp.MustCompile(`^launched T(\d+) · (.*) · session ([0-9a-f]+) · `)

func settingsOf(t *testing.T, argv []string) map[string]any {
	t.Helper()
	v := testutil.Arg(argv, "--settings")
	if v == "" {
		return nil
	}
	b, err := os.ReadFile(v)
	if err != nil {
		t.Fatalf("--settings %s is not a file: %v", v, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func jsonEq(t *testing.T, got any, want string) {
	t.Helper()
	g, _ := json.Marshal(got)
	var w any
	json.Unmarshal([]byte(want), &w)
	wb, _ := json.Marshal(w)
	if string(g) != string(wb) {
		t.Errorf("got %s\nwant %s", g, wb)
	}
}

func TestLaunchArgv(t *testing.T) {
	h := testutil.NewHome(t)
	r := ok(t, launch(t, h, "pass", "--", "--model", "opus", "--settings", `{"model":"x y"}`, "--effort", "high"))
	if m := launchedRe.FindStringSubmatch(r.stdout); m == nil || m[1] != "1" {
		t.Fatalf("stdout %q", r.stdout)
	}
	argv := h.LastArgv()
	if argv[0] != "--bg" || testutil.Arg(argv, "--name") != "T1 · pass" {
		t.Errorf("argv %q", argv)
	}
	if n := strings.Count(strings.Join(argv, "\x00"), "\x00--model\x00"); n != 1 || testutil.Arg(argv, "--model") != "opus" {
		t.Errorf("one --model, the user's: %q", argv)
	}
	if testutil.Arg(argv, "--permission-mode") != "bypassPermissions" || testutil.Arg(argv, "--effort") != "high" {
		t.Errorf("defaults and passthrough: %q", argv)
	}
	jsonEq(t, settingsOf(t, argv), `{"model":"x y","worktree":{"baseRef":"head"}}`)
	if tail := argv[len(argv)-2:]; tail[0] != "--" || tail[1] != "do it" {
		t.Errorf("prompt last after --: %q", tail)
	}
	sys := testutil.Arg(argv, "--append-system-prompt")
	if !strings.Contains(sys, "Prefer an isolated git") || strings.Contains(sys, "<!--") || !strings.HasSuffix(sys, "Your task ID is T1.\n") {
		t.Errorf("worker rules: %q", sys)
	}
	h.ResetArgv()
	ok(t, launch(t, h, "skip", "--", "--dangerously-skip-permissions"))
	if testutil.Arg(h.LastArgv(), "--permission-mode") != "" {
		t.Error("--dangerously-skip-permissions replaces the default permission mode")
	}
	for _, bad := range []string{"--name", "--bg", "--append-system-prompt=x", "-p", "--resume", "-c"} {
		r := launch(t, h, "bad", "--", bad, "v")
		if r.code != 1 || !strings.Contains(r.stderr, "is set by csquad launch and cannot be passed after --") {
			t.Errorf("%s: %+v", bad, r)
		}
	}
	r = launch(t, h, "x", "--after", "T1")
	if r.code != 1 || !strings.Contains(r.stderr, "not available in this build") {
		t.Errorf("--after: %+v", r)
	}
}

func TestLaunchIsolation(t *testing.T) {
	h := testutil.NewHome(t)
	ok(t, launch(t, h, "iso", "--isolation", "none"))
	argv := h.LastArgv()
	jsonEq(t, settingsOf(t, argv), `{"worktree":{"bgIsolation":"none"}}`)
	sys := testutil.Arg(argv, "--append-system-prompt")
	if !strings.Contains(sys, "launched to work in place") || strings.Contains(sys, "Prefer an isolated git") || strings.Contains(sys, "Commit finished code") {
		t.Errorf("in-place rules: %q", sys)
	}
	ok(t, launch(t, h, "iso", "--isolation", "none", "--", "--settings", `{"model":"sonnet","worktree":{"x":1}}`))
	argv = h.LastArgv()
	if strings.Count(strings.Join(argv, " "), "--settings") != 1 {
		t.Errorf("one --settings: %q", argv)
	}
	jsonEq(t, settingsOf(t, argv), `{"model":"sonnet","worktree":{"x":1,"bgIsolation":"none"}}`)
	// A relative --settings file is read against the launching directory.
	os.WriteFile(filepath.Join(h.Root, "my-settings.json"), []byte(`{"model":"opus"}`), 0o644)
	ok(t, csq(t, h, opts{stdin: "go"}, "launch", "--title", "f", "--cwd", h.Home, "--isolation", "none", "--", "--settings=my-settings.json"))
	jsonEq(t, settingsOf(t, h.LastArgv()), `{"model":"opus","worktree":{"bgIsolation":"none"}}`)
	if r := launch(t, h, "x", "--isolation", "sideways"); r.code != 1 || !strings.Contains(r.stderr, "--isolation must be none or worktree") {
		t.Errorf("%+v", r)
	}
	ok(t, launch(t, h, "bare", "--no-worker-rules"))
	if s := testutil.Arg(h.LastArgv(), "--append-system-prompt"); strings.TrimSpace(s) != "Your task ID is T4." {
		t.Errorf("--no-worker-rules: %q", s)
	}
}

// Claude Code branches worktrees from origin's default branch unless
// worktree.baseRef is "head"; tasks should see unpushed local commits.
func TestLaunchBaseRef(t *testing.T) {
	h := testutil.NewHome(t)
	ok(t, launch(t, h, "b", "--", "--settings", `{"worktree":{"baseRef":"fresh"}}`))
	jsonEq(t, settingsOf(t, h.LastArgv()), `{"worktree":{"baseRef":"fresh"}}`)
	os.MkdirAll(h.Claude, 0o755)
	os.WriteFile(filepath.Join(h.Claude, "settings.json"), []byte(`{"worktree":{"baseRef":"fresh"}}`), 0o644)
	ok(t, launch(t, h, "b"))
	jsonEq(t, settingsOf(t, h.LastArgv()), `{}`)
	os.Remove(filepath.Join(h.Claude, "settings.json"))
	repo := filepath.Join(h.Root, "repo")
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)
	exec.Command("git", "init", "-q", repo).Run()
	os.MkdirAll(filepath.Join(repo, ".claude"), 0o755)
	os.WriteFile(filepath.Join(repo, ".claude", "settings.local.json"), []byte(`{"worktree":{"baseRef":"fresh"}}`), 0o644)
	ok(t, csq(t, h, opts{stdin: "go"}, "launch", "--title", "b", "--cwd", filepath.Join(repo, "sub")))
	jsonEq(t, settingsOf(t, h.LastArgv()), `{}`)
}

// Claude Code's background service does not see the launching shell's
// environment: the session's provider variables go to the task through a
// 0600 settings file, all or nothing, and never onto the command line.
func TestLaunchProviderForwarding(t *testing.T) {
	h := testutil.NewHome(t)
	provider := []string{"ANTHROPIC_BASE_URL=https://proxy.example", "ANTHROPIC_AUTH_TOKEN=sk-secret", "ANTHROPIC_DEFAULT_OPUS_MODEL=gpt-a", "CLAUDE_CODE_USE_BEDROCK=1", "UNRELATED=x"}
	run := func(env []string, extra ...string) result {
		return csq(t, h, opts{stdin: "go", env: env}, append([]string{"launch", "--title", "env", "--cwd", h.Root}, extra...)...)
	}
	r := ok(t, run(provider, "--", "--settings", `{"model":"opus"}`))
	argv := h.LastArgv()
	file := testutil.Arg(argv, "--settings")
	if file != filepath.Join(h.Csquad, "settings", "T1.json") {
		t.Errorf("settings file %q", file)
	}
	if strings.Contains(strings.Join(argv, " "), "sk-secret") {
		t.Error("the token is on the command line")
	}
	jsonEq(t, settingsOf(t, argv), `{"model":"opus","worktree":{"baseRef":"head"},"env":{"ANTHROPIC_BASE_URL":"https://proxy.example","ANTHROPIC_AUTH_TOKEN":"sk-secret","ANTHROPIC_DEFAULT_OPUS_MODEL":"gpt-a","CLAUDE_CODE_USE_BEDROCK":"1"}}`)
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", st.Mode())
		}
		if st, _ := os.Stat(filepath.Dir(file)); st.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %v", st.Mode())
		}
	}
	if !strings.Contains(r.stderr, "T1 uses this session's provider settings (ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_OPUS_MODEL, CLAUDE_CODE_USE_BEDROCK)") || strings.Contains(r.stderr, "sk-secret") {
		t.Errorf("stderr %q", r.stderr)
	}
	// A task routed to another provider gets none of this session's variables.
	ok(t, run(provider, "--", "--settings", `{"env":{"ANTHROPIC_BASE_URL":"https://providerB.example"}}`))
	jsonEq(t, settingsOf(t, h.LastArgv())["env"], `{"ANTHROPIC_BASE_URL":"https://providerB.example"}`)
	// ... nor when the user's settings.json picks the provider, nor an apiKeyHelper.
	os.MkdirAll(h.Claude, 0o755)
	os.WriteFile(filepath.Join(h.Claude, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-from-settings"}}`), 0o644)
	ok(t, run(provider))
	if settingsOf(t, h.LastArgv())["env"] != nil {
		t.Error("forwarded although the user's settings choose a provider")
	}
	os.Remove(filepath.Join(h.Claude, "settings.json"))
	ok(t, run(provider, "--", "--settings", `{"apiKeyHelper":"/bin/echo k"}`))
	if settingsOf(t, h.LastArgv())["env"] != nil {
		t.Error("forwarded although an apiKeyHelper provides credentials")
	}
	// The user's own --settings env wins over a forwarded value.
	ok(t, run(provider, "--", "--settings", `{"env":{"ANTHROPIC_DEFAULT_OPUS_MODEL":"mine"}}`))
	if env := settingsOf(t, h.LastArgv())["env"].(map[string]any); env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "mine" || env["ANTHROPIC_AUTH_TOKEN"] != "sk-secret" {
		t.Errorf("env %v", env)
	}
	// A --settings file with a token is never inlined into argv.
	secret := filepath.Join(h.Root, "provider-settings.json")
	os.WriteFile(secret, []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-FILE-SECRET","ANTHROPIC_BASE_URL":"https://relay.example"}}`), 0o644)
	for _, form := range [][]string{{"--settings", secret}, {"--settings=" + secret}} {
		ok(t, run(nil, append([]string{"--"}, form...)...))
		argv := h.LastArgv()
		if strings.Contains(strings.Join(argv, " "), "sk-FILE-SECRET") {
			t.Error("file content inlined")
		}
		jsonEq(t, settingsOf(t, argv), `{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-FILE-SECRET","ANTHROPIC_BASE_URL":"https://relay.example"},"worktree":{"baseRef":"head"}}`)
	}
	// A failed launch leaves no settings file and frees its number.
	r = csq(t, h, opts{stdin: "go", env: provider}, "launch", "--title", "FAILME", "--cwd", h.Root)
	if r.code != 1 || !strings.Contains(r.stderr, "launching T8 · FAILME failed:\nworkspace not trusted") {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(h.Csquad, "settings", "T8.json")); err == nil {
		t.Error("settings file of a failed launch left")
	}
	if m := launchedRe.FindStringSubmatch(ok(t, launch(t, h, "next")).stdout); m[1] != "8" {
		t.Errorf("number after a failed launch: %s", m[1])
	}
}

func TestLaunchPromptHandling(t *testing.T) {
	h := testutil.NewHome(t)
	title := `a b "q" 's' 100% ^c & (d) | <e> !f! $g ` + "`h`" + ` \`
	prompt := "line one\nsays \"hi\" and 'bye'\r\n100% ^caret & amp | pipe <in> out\n\nC:\\dir\\ \\\\ trailing\\"
	ok(t, csq(t, h, opts{stdin: prompt}, "launch", "--title", title, "--cwd", h.Root))
	argv := h.LastArgv()
	if testutil.Arg(argv, "--name") != "T1 · "+title || argv[len(argv)-1] != prompt {
		t.Errorf("argv %q", argv)
	}
	// --prompt-file, --rm-prompt.
	file := filepath.Join(h.Root, "draft.md")
	os.WriteFile(file, []byte("from a file\nsecond line\n"), 0o644)
	ok(t, csq(t, h, opts{}, "launch", "--title", "f", "--prompt-file", file, "--cwd", h.Root))
	if a := h.LastArgv(); a[len(a)-1] != "from a file\nsecond line" {
		t.Errorf("prompt %q", a[len(a)-1])
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("kept without --rm-prompt")
	}
	os.WriteFile(file, []byte("x"), 0o644)
	if r := csq(t, h, opts{}, "launch", "--title", "FAILME", "--prompt-file", file, "--rm-prompt", "--cwd", h.Root); r.code != 1 {
		t.Error("FAILME must fail")
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("kept when the launch fails")
	}
	ok(t, csq(t, h, opts{}, "launch", "--title", "g", "--prompt-file", file, "--rm-prompt", "--cwd", h.Root))
	if _, err := os.Stat(file); err == nil {
		t.Error("--rm-prompt keeps the file")
	}
	if r := csq(t, h, opts{}, "launch", "--title", "h", "--prompt-file", file, "--cwd", h.Root); r.code != 1 || !strings.Contains(r.stderr, "cannot read the prompt file") {
		t.Errorf("%+v", r)
	}
	if r := csq(t, h, opts{stdin: "  \n"}, "launch", "--title", "h", "--cwd", h.Root); r.code != 1 || !strings.Contains(r.stderr, "the prompt (stdin) is empty") {
		t.Errorf("%+v", r)
	}
	// A prompt too large for argv goes through prompts/T<n>.md.
	big := strings.Repeat("x", 300000) + "\nEND-MARKER"
	r := ok(t, csq(t, h, opts{stdin: big, env: []string{"FAKE_MAX_ARG=100000"}}, "launch", "--title", "big", "--cwd", h.Root))
	n := launchedRe.FindStringSubmatch(r.stdout)[1]
	pf := filepath.Join(h.Csquad, "prompts", "T"+n+".md")
	a := h.LastArgv()
	if p := a[len(a)-1]; len(p) > 1000 || !strings.Contains(p, pf) {
		t.Errorf("prompt arg %q", p)
	}
	if b, _ := os.ReadFile(pf); string(b) != big+"\n" {
		t.Error("prompt file content")
	}
	// A failed large launch leaves no prompt file.
	csq(t, h, opts{stdin: big}, "launch", "--title", "FAILME", "--cwd", h.Root)
	entries, _ := os.ReadDir(filepath.Join(h.Csquad, "prompts"))
	if len(entries) != 1 {
		t.Errorf("prompts: %v", entries)
	}
}

func TestConcurrentLaunchesGetDistinctNumbers(t *testing.T) {
	h := testutil.NewHome(t)
	ok(t, launch(t, h, "existing"))
	var wg sync.WaitGroup
	nums := make([]string, 10)
	for i := range nums {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := launch(t, h, fmt.Sprintf("r%d", i))
			if m := launchedRe.FindStringSubmatch(r.stdout); m != nil {
				nums[i] = m[1]
			} else {
				nums[i] = "failed: " + r.stderr
			}
		}(i)
	}
	wg.Wait()
	sort.Slice(nums, func(i, j int) bool { return len(nums[i]) < len(nums[j]) || len(nums[i]) == len(nums[j]) && nums[i] < nums[j] })
	if strings.Join(nums, ",") != "2,3,4,5,6,7,8,9,10,11" {
		t.Errorf("numbers %v", nums)
	}
	names := map[string]bool{}
	for _, s := range h.Sessions() {
		if names[s.Name] {
			t.Errorf("duplicate session name %s", s.Name)
		}
		names[s.Name] = true
	}
}

// ---- status, peek, send ----------------------------------------------------

const sid = "abcd1234-0000-4000-8000-000000000000"

func mine(env ...string) []string { return append([]string{"CLAUDE_CODE_SESSION_ID=" + sid}, env...) }

// launchAs launches a task from the session sess and returns its number.
func launchAs(t *testing.T, h *testutil.Home, sess, title string) string {
	t.Helper()
	r := ok(t, csq(t, h, opts{stdin: "go", env: []string{"CLAUDE_CODE_SESSION_ID=" + sess}}, "launch", "--title", title, "--cwd", h.Root))
	return launchedRe.FindStringSubmatch(r.stdout)[1]
}

// setState changes the fake session of task n.
func setState(h *testutil.Home, n string, f func(*testutil.Session)) {
	list := h.Sessions()
	for i := range list {
		if strings.HasPrefix(list[i].Name, "T"+n+" · ") {
			f(&list[i])
		}
	}
	h.SetSessions(list)
}

func writeTranscript(t *testing.T, h *testutil.Home, n string, lines ...string) {
	t.Helper()
	for _, s := range h.Sessions() {
		if strings.HasPrefix(s.Name, "T"+n+" · ") {
			dir := filepath.Join(h.Claude, "projects", "-tmp-x")
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, s.SessionID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		}
	}
}

func TestStatusScopedToThisSession(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "mine")
	launchAs(t, h, "ffffffff-0000-4000-8000-000000000000", "theirs")
	own := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout
	if !strings.Contains(own, "## T1 · mine\nstate: working · started ") || strings.Contains(own, "theirs") {
		t.Errorf("own: %s", own)
	}
	if all := ok(t, csq(t, h, opts{env: mine()}, "status", "--all")).stdout; !strings.Contains(all, "## T2 · theirs") {
		t.Errorf("--all: %s", all)
	}
	if outside := ok(t, csq(t, h, opts{}, "status")).stdout; !strings.Contains(outside, "## T2 · theirs") {
		t.Errorf("outside a session every task shows: %s", outside)
	}
	if none := ok(t, csq(t, h, opts{env: []string{"CLAUDE_CODE_SESSION_ID=eeee"}}, "status")).stdout; !strings.Contains(none, "No tasks launched by this session") {
		t.Errorf("none: %s", none)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(ok(t, csq(t, h, opts{env: mine()}, "status", "--json")).stdout), &list); err != nil || len(list) != 1 || list[0]["id"] != "T1" || list[0]["state"] != "working" {
		t.Errorf("--json: %v %v", list, err)
	}
}

func TestStatesFollowAgentView(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "x")
	for _, c := range []struct {
		state, status, tempo string
		want                 string
	}{
		{"done", "idle", "", "done"}, {"crashed", "", "", "failed"}, {"working", "shell", "", "working"},
		{"working", "waiting", "", "needs input"}, {"working", "idle", "blocked", "needs input"},
		{"blocked", "", "", "needs input"}, {"working", "idle", "", "idle"}, {"working", "", "", "working"},
		{"stopped", "busy", "", "stopped"},
	} {
		setState(h, n, func(s *testutil.Session) { s.State, s.Status, s.Tempo = c.state, c.status, c.tempo })
		out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout
		if !strings.Contains(out, "state: "+c.want+" ") {
			t.Errorf("%+v: %s", c, out)
		}
	}
	h.SetSessions(nil)
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; !strings.Contains(out, "state: gone ") {
		t.Errorf("removed session: %s", out)
	}
}

func TestPeekFoldsToolsAndRefusesOtherSessions(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "mine")
	other := launchAs(t, h, "ffffffff-0000-4000-8000-000000000000", "theirs")
	writeTranscript(t, h, n,
		`{"type":"user","message":{"content":"please fix it"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"On it."},{"type":"tool_use","name":"Bash"},{"type":"tool_use","name":"Read"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"user","message":{"content":"<system-reminder>x</system-reminder>"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"content":"sub"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Done: branch worktree-t1-x"}]}}`)
	out := ok(t, csq(t, h, opts{env: mine()}, "peek", "T"+n)).stdout
	want := "T1 · mine · working · session "
	if !strings.HasPrefix(out, want) || !strings.Contains(out, "[user]\nplease fix it\n\n[assistant]\nOn it.\n\n[tools] Bash ×2, Read ×1\n\n[assistant]\nDone: branch worktree-t1-x\n") || strings.Contains(out, "sub") {
		t.Errorf("peek:\n%s", out)
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "peek", "T1", "-n", "1")).stdout; strings.Contains(out, "On it") {
		t.Errorf("-n 1: %s", out)
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; !strings.Contains(out, "latest:\nDone: branch worktree-t1-x") {
		t.Errorf("status latest: %s", out)
	}
	r := csq(t, h, opts{env: mine()}, "peek", "T"+other)
	if r.code != 1 || !strings.Contains(r.stderr, "T2 was launched by another session; pass --all to peek it anyway") {
		t.Errorf("%+v", r)
	}
	ok(t, csq(t, h, opts{env: mine()}, "peek", "T"+other, "--all"))
	if r := csq(t, h, opts{}, "peek", "T9"); r.code != 1 || !strings.Contains(r.stderr, "T9 is not a csquad task") {
		t.Errorf("%+v", r)
	}
	if r := csq(t, h, opts{}, "peek", "banana"); r.code != 1 || !strings.Contains(r.stderr, "expected a task like T12") {
		t.Errorf("%+v", r)
	}
}

func TestSendWakesAStoppedSession(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "mine")
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "" })
	r := ok(t, csq(t, h, opts{env: mine()}, "send", "T"+n, "also", "handle", "empty input"))
	argv := h.LastArgv()
	if !strings.Contains(r.stdout, "woke session") || testutil.Arg(argv, "--resume") == "" || argv[len(argv)-1] != "also handle empty input" {
		t.Errorf("%+v %q", r, argv)
	}
	if !strings.HasSuffix(testutil.Arg(argv, "--resume"), "-0000-4000-8000-000000000000") {
		t.Errorf("--resume needs the full session id: %q", argv)
	}
}

// ---- wait ------------------------------------------------------------------

var fast = []string{"CSQUAD_WAIT_INTERVAL_MS=30", "CSQUAD_WAIT_GRACE_MS=200"}

type step struct {
	after time.Duration
	f     func()
}

func waitFor(t *testing.T, h *testutil.Home, env []string, script []step, args ...string) result {
	t.Helper()
	done := make(chan result, 1)
	go func() { done <- csq(t, h, opts{env: append(append(mine(), fast...), env...)}, append([]string{"wait"}, args...)...) }()
	start := time.Now()
	for _, s := range script {
		time.Sleep(time.Until(start.Add(s.after)))
		s.f()
	}
	select {
	case r := <-done:
		return r
	case <-time.After(15 * time.Second):
		t.Fatal("wait did not exit")
	}
	return result{}
}

func TestWaitReportsChanges(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "fix login")
	for _, c := range []struct{ state, status, want string }{{"done", "", "done"}, {"blocked", "", "needs input"}, {"failed", "", "failed"}, {"stopped", "", "stopped"}, {"working", "idle", "idle"}} {
		setState(h, n, func(s *testutil.Session) { s.State, s.Status = "working", "busy" })
		r := waitFor(t, h, nil, []step{{200 * time.Millisecond, func() {
			setState(h, n, func(s *testutil.Session) { s.State, s.Status = c.state, c.status })
		}}}, "T"+n)
		if r.code != 0 || r.stdout != "T1 · fix login · "+c.want+"\n" {
			t.Errorf("%s: %+v", c.want, r)
		}
	}
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "working", "busy" })
	r := waitFor(t, h, nil, []step{{200 * time.Millisecond, func() { h.SetSessions(nil) }}}, "1")
	if r.stdout != "T1 · fix login · removed\n" {
		t.Errorf("removed: %+v", r)
	}
}

// A new state counts only when seen on two consecutive polls. The fake's
// n-th `claude agents` call returns the n-th snapshot.
func TestWaitTwoObservations(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "flaky")
	seq := func(states ...string) string {
		var snaps []string
		for _, st := range states {
			setState(h, n, func(s *testutil.Session) { s.State, s.Status = st, "" })
			b, _ := os.ReadFile(h.State)
			snaps = append(snaps, string(b))
		}
		os.Remove(h.State + ".seq")
		return "FAKE_SEQ=[" + strings.Join(snaps, ",") + "]"
	}
	// Poll 0 is the start state; done on one poll only, then failed for good.
	r := waitFor(t, h, []string{seq("working", "working", "done", "working", "working", "failed", "failed")}, nil, "T"+n)
	if r.stdout != "T1 · flaky · failed\n" {
		t.Errorf("%+v", r)
	}
	r = waitFor(t, h, []string{seq("working", "done", "done")}, nil, "T"+n)
	if r.stdout != "T1 · flaky · done\n" {
		t.Errorf("%+v", r)
	}
}

func TestWaitAlreadyUnknownOthersTimeoutFailures(t *testing.T) {
	h := testutil.NewHome(t)
	a := launchAs(t, h, sid, "old job")
	b := launchAs(t, h, sid, "busy")
	theirs := launchAs(t, h, "ffffffff-0000-4000-8000-000000000000", "theirs")
	setState(h, a, func(s *testutil.Session) { s.State = "done" })
	r := waitFor(t, h, []string{"CSQUAD_WAIT_INTERVAL_MS=60000", "CSQUAD_WAIT_GRACE_MS=50"}, nil, "T"+a, "T"+b)
	if r.stdout != "T1 · old job · already done\n" {
		t.Errorf("already: %+v", r)
	}
	if r := waitFor(t, h, nil, nil, "T9"); r.code != 1 || !strings.Contains(r.stderr, "T9 is not a csquad task") {
		t.Errorf("unknown: %+v", r)
	}
	if r := waitFor(t, h, nil, nil, "T"+theirs); r.code != 1 || !strings.Contains(r.stderr, "T3 was launched by another session; pass --all") {
		t.Errorf("others: %+v", r)
	}
	if r := waitFor(t, h, nil, nil, "T"+b, "--timeout", "0.01"); r.stdout != "T2 · busy · still working after 1m\n" && r.stdout != "T2 · busy · still working after 0m\n" {
		t.Errorf("timeout: %+v", r)
	}
	if r := waitFor(t, h, []string{"CSQUAD_WAIT_FAIL_MS=300", "FAKE_AGENTS_FAIL=1"}, nil, "T"+b); r.code != 1 || !strings.Contains(r.stderr, "keeps failing") {
		t.Errorf("failing: %+v", r)
	}
}

func TestWaitSIGTERMIsQuiet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM")
	}
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "x")
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "wait", "T"+n)
	cmd.Env = h.Env(append(append(mine(), fast...), "CSQUAD_TEST_MAIN=1")...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Start()
	time.Sleep(400 * time.Millisecond)
	cmd.Process.Signal(os.Interrupt)
	err := cmd.Wait()
	if err != nil || out.String() != "" {
		t.Errorf("err %v, output %q", err, out.String())
	}
}
