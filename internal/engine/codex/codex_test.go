package codex

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/engine/codex/codextest"
)

func TestMain(m *testing.M) {
	codextest.MaybeFakeCodex()
	os.Exit(m.Run())
}

type fixture struct {
	srv *codextest.Server
	eng *Engine
	bin string
}

// setup makes a temporary CODEX_HOME with a fake app-server (linked: a
// running daemon) and a fake `codex` on PATH.
func setup(t *testing.T, linked bool, env ...string) *fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	bin := filepath.Join(root, "bin")
	codextest.LinkFake(t, bin)
	srv := codextest.New(t, home, linked)
	e := append([]string{"HOME=" + root, "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, srv.Env()...)
	return &fixture{srv: srv, eng: New(engine.Config{Home: filepath.Join(root, "csquad"), Env: append(e, env...)}), bin: bin}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func inputText(t *testing.T, p map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(p["input"])
	var in []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &in) != nil || len(in) != 1 || in[0].Type != "text" {
		t.Fatalf("input %s", b)
	}
	return in[0].Text
}

func (f *fixture) state(t *testing.T, id string) string {
	t.Helper()
	got, err := f.eng.States(ctx(t), []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return got[id].State
}

func TestStartSendsThreadOptionsRulesNameAndPrompt(t *testing.T) {
	f := setup(t, true)
	args, err := f.eng.CheckArgs([]string{"-m", "gpt-x", "--effort=high", "-c", "a.b=1", "-c", `features={"x":true}`, "-c", "name=plain"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 7, Title: "fix it", Cwd: "/repo", Model: "ignored", Prompt: "the prompt", Rules: "RULES", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.srv.Methods(); !reflect.DeepEqual(got, []string{"thread/start", "thread/name/set", "turn/start"}) {
		t.Fatalf("calls %v", got)
	}
	start := f.srv.Last("thread/start")
	want := map[string]any{"cwd": "/repo", "approvalPolicy": "never", "sandbox": "danger-full-access", "model": "gpt-x",
		"developerInstructions": "RULES\nYour task ID is T7.\n", "config": map[string]any{"a.b": 1.0, "features": map[string]any{"x": true}, "name": "plain"}}
	if !reflect.DeepEqual(start, want) {
		t.Errorf("thread/start %v\nwant %v", start, want)
	}
	th := f.srv.Thread(res.SessionID)
	if th == nil || th.Name != "T7 · fix it" {
		t.Fatalf("thread %+v", th)
	}
	turn := f.srv.Last("turn/start")
	if inputText(t, turn) != "the prompt" || turn["effort"] != "high" || turn["threadId"] != res.SessionID || !strings.HasPrefix(turn["clientUserMessageId"].(string), "csquad-") {
		t.Errorf("turn/start %v", turn)
	}
	if got := f.state(t, res.SessionID); got != engine.Working {
		t.Errorf("state %s", got)
	}
	// Defaults and the launch's --model; options after -- win over defaults.
	f.srv.Reset()
	if _, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 8, Title: "b", Cwd: "/r", Model: "gpt-y", Prompt: "p", Args: []string{"--sandbox", "read-only", "--ask-for-approval", "on-request"}}); err != nil {
		t.Fatal(err)
	}
	start = f.srv.Last("thread/start")
	if start["model"] != "gpt-y" || start["sandbox"] != "read-only" || start["approvalPolicy"] != "on-request" || start["developerInstructions"] != nil || start["config"] != nil {
		t.Errorf("thread/start %v", start)
	}
	if _, ok := f.srv.Last("turn/start")["effort"]; ok {
		t.Error("effort without --effort")
	}
}

func TestStartArchivesAThreadItCouldNotStart(t *testing.T) {
	f := setup(t, true)
	f.srv.Fail("turn/start", "model not available")
	_, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "model not available") || !strings.Contains(err.Error(), "T1 · x") {
		t.Fatalf("%v", err)
	}
	id, _ := f.srv.Last("thread/archive")["threadId"].(string)
	if th := f.srv.Thread(id); th == nil || !th.Archived {
		t.Errorf("the thread was not archived: %v", f.srv.Methods())
	}
}

func TestStatesFollowTheThreadAndItsNewestTurn(t *testing.T) {
	f := setup(t, true)
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	f.srv.Edit(id, func(th *codextest.Thread) { th.Flags = []string{"waitingOnApproval"} })
	if got := f.state(t, id); got != engine.NeedsInput {
		t.Errorf("waiting on approval: %s", got)
	}
	f.srv.Complete(id, "Done.\n\nShould I also update the README?")
	if got := f.state(t, id); got != engine.NeedsInput {
		t.Errorf("ended with a question: %s", got)
	}
	f.srv.Edit(id, func(th *codextest.Thread) {
		th.Turns[0].Items[len(th.Turns[0].Items)-1]["text"] = "All done, committed abc123."
	})
	if got := f.state(t, id); got != engine.Idle {
		t.Errorf("completed: %s", got)
	}
	f.srv.Edit(id, func(th *codextest.Thread) { th.Status = "systemError" })
	if got := f.state(t, id); got != engine.Failed {
		t.Errorf("system error: %s", got)
	}
	f.srv.Edit(id, func(th *codextest.Thread) { th.Status = "idle"; th.Turns[0].Status = "interrupted" })
	if got := f.state(t, id); got != engine.Stopped {
		t.Errorf("interrupted: %s", got)
	}
	// The daemon unloaded it: the newest turn still decides.
	f.srv.Edit(id, func(th *codextest.Thread) { th.Turns[0].Status = "completed" })
	f.srv.Unload(id)
	got, err := f.eng.States(ctx(t), []string{id, "019a0000-0000-7000-8000-000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].State != engine.Idle || got[id].SessionID != id || got[id].StartedAt.IsZero() || got[id].Cwd != "" {
		t.Errorf("unloaded: %+v", got[id])
	}
	if got["019a0000-0000-7000-8000-000000000000"].State != engine.Gone {
		t.Errorf("unknown thread: %+v", got)
	}
}

func TestLiveMapping(t *testing.T) {
	now := time.Now()
	young, old := codextest.NewID(now.Add(-30*time.Second)), codextest.NewID(now.Add(-3*time.Minute))
	user := Item{Type: "userMessage"}
	for _, c := range []struct {
		name   string
		status ThreadStatus
		turn   *Turn
		want   string
	}{
		{"active", ThreadStatus{Type: "active"}, nil, engine.Working},
		{"approval", ThreadStatus{Type: "active", ActiveFlags: []string{"waitingOnApproval"}}, nil, engine.NeedsInput},
		{"user input", ThreadStatus{Type: "active", ActiveFlags: []string{"waitingOnUserInput"}}, nil, engine.NeedsInput},
		{"system error", ThreadStatus{Type: "systemError"}, nil, engine.Failed},
		{"no turn", ThreadStatus{Type: "idle"}, nil, engine.Idle},
		{"idle but turn running", ThreadStatus{Type: "idle"}, &Turn{Status: "inProgress"}, engine.Working},
		{"failed", ThreadStatus{Type: "idle"}, &Turn{Status: "failed"}, engine.Failed},
		{"young interrupted turn without its message", ThreadStatus{Type: "idle"}, &Turn{ID: young, Status: "interrupted"}, engine.Working},
		{"old interrupted turn", ThreadStatus{Type: "idle"}, &Turn{ID: old, Status: "interrupted"}, engine.Stopped},
		{"young interrupted turn with its message", ThreadStatus{Type: "idle"}, &Turn{ID: young, Status: "interrupted", Items: []Item{user}}, engine.Stopped},
		{"completed", ThreadStatus{Type: "notLoaded"}, &Turn{Status: "completed", Items: []Item{user, {Type: "agentMessage", Text: "Is it? No. Done."}}}, engine.Idle},
		{"question", ThreadStatus{Type: "idle"}, &Turn{Status: "completed", Items: []Item{{Type: "agentMessage", Text: "Which one, **A or B?**"}}}, engine.NeedsInput},
		{"questions field", ThreadStatus{Type: "idle"}, &Turn{Status: "completed", Items: []Item{{Type: "agentMessage", Text: "Pick.", Questions: []json.RawMessage{[]byte(`{}`)}}}}, engine.NeedsInput},
	} {
		if got := Live(Thread{Status: c.status}, c.turn, now); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSendSteersARunningTurnElseStartsOne(t *testing.T) {
	f := setup(t, true)
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	turnID := f.srv.Thread(id).Turns[0].ID
	f.srv.Reset()
	how, err := f.eng.Send(ctx(t), id, "also this")
	if err != nil {
		t.Fatal(err)
	}
	steer := f.srv.Last("turn/steer")
	if steer == nil || steer["expectedTurnId"] != turnID || inputText(t, steer) != "also this" || f.srv.Last("turn/start") != nil || !strings.Contains(how, "running turn") {
		t.Errorf("steer: %v %v (%s)", f.srv.Methods(), steer, how)
	}
	// The turn ended between the read and the steer: a new turn.
	f.srv.Reset()
	f.srv.Fail("turn/steer", "no active turn")
	if _, err := f.eng.Send(ctx(t), id, "race"); err != nil {
		t.Fatal(err)
	}
	if f.srv.Last("turn/start") == nil {
		t.Errorf("no turn/start after a failed steer: %v", f.srv.Methods())
	}
	f.srv.Fail("turn/steer", "")
	// Idle: a new turn, no resume.
	f.srv.Complete(id, "done")
	f.srv.Reset()
	if how, err = f.eng.Send(ctx(t), id, "next"); err != nil {
		t.Fatal(err)
	}
	if m := f.srv.Methods(); !reflect.DeepEqual(m, []string{"thread/read", "turn/start"}) || inputText(t, f.srv.Last("turn/start")) != "next" || !strings.HasPrefix(how, "started a new turn") {
		t.Errorf("idle: %v (%s)", m, how)
	}
	// Unloaded: thread/resume first.
	f.srv.Complete(id, "done")
	f.srv.Unload(id)
	f.srv.Reset()
	if how, err = f.eng.Send(ctx(t), id, "again"); err != nil {
		t.Fatal(err)
	}
	if m := f.srv.Methods(); !reflect.DeepEqual(m, []string{"thread/read", "thread/resume", "turn/start"}) || !strings.HasPrefix(how, "resumed the thread") {
		t.Errorf("unloaded: %v (%s)", m, how)
	}
	if _, err := f.eng.Send(ctx(t), "019a0000-0000-7000-8000-000000000000", "x"); err == nil || !strings.Contains(err.Error(), "no longer has thread") {
		t.Errorf("unknown thread: %v", err)
	}
}

func TestLastFoldsToolsAndPages(t *testing.T) {
	f := setup(t, true)
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "first prompt"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	f.srv.Edit(id, func(th *codextest.Thread) {
		th.Turns[0].Items = append(th.Turns[0].Items, map[string]any{"type": "reasoning"},
			map[string]any{"type": "commandExecution"}, map[string]any{"type": "commandExecution"},
			map[string]any{"type": "mcpToolCall", "tool": "search"}, map[string]any{"type": "agentMessage", "text": "one"})
		th.Turns[0].Status, th.Status = "completed", "idle"
	})
	for i := 0; i < 6; i++ { // more turns than one page holds
		if _, err := f.eng.Send(ctx(t), id, "more"); err != nil {
			t.Fatal(err)
		}
		f.srv.Complete(id, "reply")
	}
	f.srv.Complete(id, "last reply")
	all, err := f.eng.Last(ctx(t), id, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 || all[0].Role != "user" || all[0].Text != "first prompt" || all[1].Role != "tools" ||
		!reflect.DeepEqual(all[1].Tools, []engine.ToolCount{{Name: "commandExecution", Count: 2}, {Name: "search", Count: 1}}) || all[2].Text != "one" {
		t.Fatalf("%+v", all)
	}
	last, err := f.eng.Last(ctx(t), id, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 2 || last[0].Role != "tools" || last[1].Text != "last reply" {
		t.Errorf("last 2: %+v", last)
	}
	f.srv.Reset()
	latest, err := f.eng.Last(ctx(t), id, 0)
	if err != nil || engine.LastAssistant(latest) != "last reply" || len(f.srv.Methods()) != 1 {
		t.Errorf("n=0 reads one page: %+v %v %v", latest, err, f.srv.Methods())
	}
}

func TestStopAndRemove(t *testing.T) {
	f := setup(t, true)
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	if err := f.eng.Stop(ctx(t), id); err != nil {
		t.Fatal(err)
	}
	if got := f.srv.Last("turn/interrupt"); got["turnId"] != f.srv.Thread(id).Turns[0].ID || f.state(t, id) != engine.Stopped {
		t.Errorf("interrupt %v", got)
	}
	how, err := f.eng.Remove(ctx(t), id, nil)
	if err != nil || !f.srv.Thread(id).Archived || how != "archived thread "+id {
		t.Fatalf("remove: %q %v", how, err)
	}
	if how, err := f.eng.Remove(ctx(t), id, nil); err != nil || !strings.Contains(how, "already removed") {
		t.Errorf("remove twice: %q %v", how, err)
	}
	if got := f.eng.Takeover(id); !reflect.DeepEqual(got, []string{"codex", "resume", id}) {
		t.Errorf("takeover %v", got)
	}
}

func TestDialStartsTheDaemonWhenTheSocketIsMissing(t *testing.T) {
	f := setup(t, false)
	if _, err := f.eng.States(ctx(t), []string{"019a0000-0000-7000-8000-000000000000"}); err != nil {
		t.Fatal(err)
	}
	if f.srv.DaemonStarts() != 1 {
		t.Errorf("daemon starts: %d", f.srv.DaemonStarts())
	}
	// Running: no second start.
	if _, err := f.eng.States(ctx(t), []string{"019a0000-0000-7000-8000-000000000000"}); err != nil || f.srv.DaemonStarts() != 1 {
		t.Errorf("%v, starts %d", err, f.srv.DaemonStarts())
	}
}

func TestDialStartsTheDaemonWhenTheSocketRefuses(t *testing.T) {
	f := setup(t, false)
	// The link points at a socket file nobody listens on (a daemon that died).
	dir, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dead := filepath.Join(dir, "dead.sock")
	ln, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if err := codextest.LinkSocket(f.srv.Link, dead); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.States(ctx(t), []string{"019a0000-0000-7000-8000-000000000000"}); err != nil {
		t.Fatal(err)
	}
	if f.srv.DaemonStarts() != 1 {
		t.Errorf("daemon starts: %d", f.srv.DaemonStarts())
	}
}

// For a long CODEX_HOME the control socket is a link to a short path: a
// Unix socket path is limited to about 104 bytes, so csquad dials the target.
func TestDialResolvesALinkedSocket(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, strings.Repeat("long-codex-home-", 8))
	srv := codextest.New(t, home, true)
	if len(srv.Link) < 110 {
		t.Fatalf("path too short for the test: %d", len(srv.Link))
	}
	bin := filepath.Join(root, "bin")
	codextest.LinkFake(t, bin)
	e := New(engine.Config{Env: append([]string{"HOME=" + root, "PATH=" + bin}, srv.Env()...)})
	if _, err := e.States(ctx(t), []string{"019a0000-0000-7000-8000-000000000000"}); err != nil {
		t.Fatal(err)
	}
	if srv.DaemonStarts() != 0 {
		t.Errorf("a running daemon was started again")
	}
}

func TestDialErrors(t *testing.T) {
	f := setup(t, false)
	f.eng.cfg.NoStart = true
	_, err := f.eng.States(ctx(t), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "not reachable") || f.srv.DaemonStarts() != 0 {
		t.Errorf("NoStart: %v, starts %d", err, f.srv.DaemonStarts())
	}
	f.eng.cfg.NoStart = false
	f.eng.cfg.Env = append(f.eng.cfg.Env, "FAKE_CODEX_START_FAILS=1")
	_, err = f.eng.States(ctx(t), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "starting it failed") || !strings.Contains(err.Error(), "cannot start the daemon") {
		t.Errorf("start fails: %v", err)
	}
	// No ids: no connection at all.
	f.eng.cfg.NoStart = true
	if got, err := f.eng.States(ctx(t), nil); err != nil || len(got) != 0 {
		t.Errorf("%v %v", got, err)
	}
}

func TestParseOptions(t *testing.T) {
	o, err := ParseOptions([]string{"-mgpt-x", "--effort", "low", "-s", "workspace-write", "--ask-for-approval=untrusted", "--config", "k=v", "-c", "n=2", "-c", `s="7"`})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "gpt-x", "--effort", "low", "--sandbox", "workspace-write", "--ask-for-approval", "untrusted", "--config", "k=v", "--config", "n=2", "--config", `s="7"`}
	if got := o.Args(); !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
	again, err := ParseOptions(o.Args())
	if err != nil || !reflect.DeepEqual(again.Config, o.Config) || again.Config["s"] != "7" || again.Config["n"] != 2.0 {
		t.Errorf("round trip: %v %v", again.Config, err)
	}
	if got := (Options{}).Args(); got == nil || len(got) != 0 {
		t.Errorf("empty: %#v", got)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--yolo"}, "--yolo is not a Codex thread option"},
		{[]string{"prompt text"}, "is not a Codex thread option"},
		{[]string{"-m"}, "-m needs a value"},
		{[]string{"--sandbox", "full"}, `--sandbox "full": expected one of read-only, workspace-write, danger-full-access`},
		{[]string{"-a", "always"}, `--ask-for-approval "always"`},
		{[]string{"--effort", "huge"}, `--effort "huge"`},
		{[]string{"-c", "novalue"}, "expected KEY=VALUE"},
		{[]string{"-c", "=1"}, "expected KEY=VALUE"},
		{[]string{"--model="}, "--model needs a value"},
	} {
		if _, err := ParseOptions(c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
}
