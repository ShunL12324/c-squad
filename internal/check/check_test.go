package check

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.MaybeFakeClaude()
	os.Exit(m.Run())
}

func TestParseSpec(t *testing.T) {
	for in, want := range map[string]Spec{
		"codex":             {Kind: Codex},
		"codex:gpt-5.5":     {Kind: Codex, Model: "gpt-5.5"},
		"claude:opus":       {Kind: Claude, Model: "opus"},
		"cmd:go test ./...": {Kind: Cmd, Command: "go test ./..."},
		"cmd:a:b":           {Kind: Cmd, Command: "a:b"},
		" claude ":          {Kind: Claude},
	} {
		if got, err := ParseSpec(in); err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"cmd:", "cmd: ", "gemini", ""} {
		if _, err := ParseSpec(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	for _, c := range []struct {
		text string
		ok   bool
		want string
	}{
		{`{"verdict":"pass","summary":"ok","findings":[]}`, true, "pass"},
		{"Here it is:\n```json\n{\"verdict\": \"fail\", \"summary\": \"x\", \"findings\": [{\"severity\":\"blocking\",\"file\":\"a\",\"summary\":\"b\"}]}\n```", true, "fail"},
		{`I looked at it. {"note": 1} The verdict: {"verdict":"fail","summary":"two {braces} in prose","findings":[]} done`, true, "fail"},
		{`{"verdict":"maybe"}`, false, ""},
		{"looks good to me", false, ""},
		{`{"verdict":"pass"} and later {"verdict":"fail","summary":"second thoughts"}`, true, "fail"},
	} {
		v, ok := Parse(c.text)
		if ok != c.ok || v.Verdict != c.want {
			t.Errorf("%q: %+v %v", c.text, v, ok)
		}
	}
	v, _ := Parse(`{"verdict":"fail","summary":"s","findings":[{"severity":"blocking","file":"a.go","summary":"x"},{"severity":"minor","file":"","summary":"y"},{"summary":"z"}]}`)
	if v.Count() != 2 {
		t.Errorf("blocking count %d", v.Count())
	}
	if !json.Valid(Schema) {
		t.Error("the schema is not JSON")
	}
}

func TestCmdCheck(t *testing.T) {
	dir := t.TempDir()
	env := proc.Env(os.Environ())
	run := func(command string, timeout time.Duration) Verdict {
		return Run(context.Background(), Spec{Kind: Cmd, Command: command}, Request{Dir: dir, Env: env, Timeout: timeout})
	}
	os.WriteFile(filepath.Join(dir, "hello.txt"), nil, 0o644)
	if v := run("test -f hello.txt", 0); !v.Pass() {
		t.Errorf("runs in the worktree: %+v", v)
	}
	v := run("echo first; echo the error >&2; exit 3", 0)
	if v.Verdict != "fail" || v.Summary != "`echo first; echo the error >&2; exit 3` exited 3" || len(v.Findings) != 1 || v.Findings[0].Summary != "first\nthe error" {
		t.Errorf("%+v", v)
	}
	long := run("i=0; while [ $i -lt 200 ]; do echo line $i; i=$((i+1)); done; exit 1", 0)
	if f := long.Findings[0].Summary; !strings.HasPrefix(f, "line 140\n") || !strings.HasSuffix(f, "line 199") {
		t.Errorf("tail: %q", f)
	}
	start := time.Now()
	if v := run("sleep 30", 300*time.Millisecond); v.Error == "" || !strings.Contains(v.Error, "did not finish within") || time.Since(start) > 10*time.Second {
		t.Errorf("timeout: %+v", v)
	}
}

func TestClaudeReviewer(t *testing.T) {
	h := testutil.NewHome(t)
	env := proc.Env(h.Env())
	req := Request{ID: 12, Title: "fix", Prompt: "Fix the bug.", Final: "Fixed it on worktree-t12-x.", Dir: h.Root, Branch: "worktree-t12-x",
		Target: "main", Round: 2, Max: 3, Env: env, Previous: &Verdict{Verdict: "fail", Findings: []Finding{{Severity: "blocking", File: "a.go", Summary: "nil deref"}}}}
	v := Run(context.Background(), Spec{Kind: Claude, Model: "opus"}, req)
	if !v.Pass() {
		t.Fatalf("%+v", v)
	}
	argv := h.LastArgv()
	for _, want := range []string{"--output-format json", "--permission-mode dontAsk", "--no-session-persistence", "--tools Read,Grep,Glob,Bash", "--model opus", "Bash(git diff:*)"} {
		if !strings.Contains(strings.Join(argv, " "), want) {
			t.Errorf("argv lacks %q: %q", want, argv)
		}
	}
	if testutil.Arg(argv, "--json-schema") != string(Schema) {
		t.Error("no schema")
	}
	if strings.Contains(strings.Join(argv, " "), "git -C") {
		t.Error("git -C does not match the allowed tools")
	}
	prompts := h.ReviewPrompts()
	if len(prompts) != 1 {
		t.Fatalf("prompt on stdin: %q", prompts)
	}
	for _, want := range []string{"T12", "check round 2 of 3", "Fix the bug.", "Fixed it on worktree-t12-x.", "git diff main...HEAD", "[blocking] a.go: nil deref", "without -C"} {
		if !strings.Contains(prompts[0], want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompts[0])
		}
	}
	h.SetReview(`{"type":"result","is_error":false,"result":"Verdict below.\n{\"verdict\":\"fail\",\"summary\":\"no\",\"findings\":[]}"}`)
	if v := Run(context.Background(), Spec{Kind: Claude}, req); v.Verdict != "fail" || len(v.Findings) != 1 {
		t.Errorf("from the result text: %+v", v)
	}
	h.SetReview(`{"type":"result","is_error":false,"result":"I could not decide."}`)
	if v := Run(context.Background(), Spec{Kind: Claude}, req); v.Error == "" || !strings.Contains(v.Error, "no verdict") {
		t.Errorf("no verdict: %+v", v)
	}
	h.SetReview(`not json`)
	if v := Run(context.Background(), Spec{Kind: Claude}, req); !strings.Contains(v.Error, "printed no result") {
		t.Errorf("garbage: %+v", v)
	}
}

// fakeEngine is an in-memory engine standing in for Codex: a session
// answers answer after two polls.
type fakeEngine struct {
	mu      sync.Mutex
	answer  string
	polls   int
	spec    engine.StartSpec
	removed []string
	fail    bool
}

func (f *fakeEngine) Name() string { return "codex" }
func (f *fakeEngine) Mark() string { return "⬡" }
func (f *fakeEngine) CheckArgs(args []string, _ string) ([]string, error) {
	return args, nil
}
func (f *fakeEngine) Start(_ context.Context, s engine.StartSpec) (engine.Started, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spec = s
	return engine.Started{SessionID: "thread-1"}, nil
}
func (f *fakeEngine) States(_ context.Context, ids []string) (map[string]engine.Live, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	st := engine.Working
	if f.polls > 2 {
		st = engine.Idle
		if f.fail {
			st = engine.Failed
		}
	}
	return map[string]engine.Live{ids[0]: {State: st}}, nil
}
func (f *fakeEngine) Send(context.Context, string, string) (string, error) { return "", nil }
func (f *fakeEngine) Last(context.Context, string, int) ([]engine.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.polls <= 2 {
		return nil, nil
	}
	return []engine.Message{{Role: "user", Text: "review"}, {Role: "assistant", Text: f.answer}}, nil
}
func (f *fakeEngine) Stop(context.Context, string) error { return nil }
func (f *fakeEngine) Remove(_ context.Context, id string, _ []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return "archived " + id, nil
}
func (f *fakeEngine) Takeover(string) []string { return nil }

func TestCodexReviewer(t *testing.T) {
	f := &fakeEngine{answer: `{"verdict":"fail","summary":"missing test","findings":[{"severity":"blocking","file":"x_test.go","summary":"add one"}]}`}
	req := Request{ID: 12, Title: "fix", Prompt: "p", Dir: "/w", Branch: "b", Target: "main", Round: 1, Max: 3, Poll: time.Millisecond,
		Engine: func(name string) (engine.Engine, error) { return f, nil }}
	v := Run(context.Background(), Spec{Kind: Codex, Model: "gpt-x"}, req)
	if v.Verdict != "fail" || v.Count() != 1 {
		t.Fatalf("%+v", v)
	}
	if f.spec.Name() != "T12 · check 1" || f.spec.Model != "gpt-x" || f.spec.Cwd != "/w" || f.spec.Rules != "" || string(f.spec.Schema) != string(Schema) ||
		strings.Join(f.spec.Args, " ") != "-s read-only -a never" {
		t.Errorf("start spec %+v", f.spec)
	}
	if len(f.removed) != 1 || f.removed[0] != "thread-1" {
		t.Errorf("the reviewer thread is archived: %v", f.removed)
	}
	f2 := &fakeEngine{fail: true}
	req.Engine = func(string) (engine.Engine, error) { return f2, nil }
	if v := Run(context.Background(), Spec{Kind: Codex}, req); v.Error != "the reviewer failed" || len(f2.removed) != 1 {
		t.Errorf("%+v %v", v, f2.removed)
	}
	f3 := &fakeEngine{answer: "LGTM"}
	req.Engine = func(string) (engine.Engine, error) { return f3, nil }
	if v := Run(context.Background(), Spec{Kind: Codex}, req); !strings.HasPrefix(v.Error, "the reviewer gave no verdict") {
		t.Errorf("%+v", v)
	}
}
