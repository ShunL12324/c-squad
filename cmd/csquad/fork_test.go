package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

// --fork starts the task as a fork of the dispatching session, by its full
// id, in the same engine; without that session it is refused before
// anything starts.
func TestLaunchFork(t *testing.T) {
	h := testutil.NewHome(t)
	// The dispatching session's transcript, with usage.
	parent := `{"type":"assistant","message":{"id":"p1","usage":{"input_tokens":10,"cache_creation_input_tokens":90000,"cache_read_input_tokens":0,"output_tokens":5000}}}`
	dir := filepath.Join(h.Claude, "projects", "-tmp-x")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(parent+"\n"), 0o644)
	r := ok(t, csq(t, h, opts{stdin: "go on from here", env: mine()}, "launch", "--title", "forked", "--cwd", h.Root, "--fork"))
	if !strings.Contains(r.stdout, "a fork of this session") {
		t.Errorf("%s", r.stdout)
	}
	argv := h.LastArgv()
	if strings.Join(argv[:6], " ") != "--bg --resume "+sid+" --fork-session --name T1 · forked" || argv[len(argv)-1] != "go on from here" ||
		testutil.Arg(argv, "--append-system-prompt") == "" || testutil.Arg(argv, "--settings") == "" {
		t.Errorf("argv %q", argv)
	}
	if len(h.Sessions()) != 1 {
		t.Errorf("the fork is a new session: %+v", h.Sessions())
	}
	// The fork's transcript carries the parent's history, usage included:
	// that much is its base, not its work.
	base := filepath.Join(h.Csquad, "cache", "T1.base.json")
	if !exists(base) {
		t.Fatal("no usage base recorded for the fork")
	}
	writeTranscript(t, h, "1", parent,
		`{"type":"assistant","message":{"id":"f1","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":95000,"output_tokens":700}}}`)
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; !strings.Contains(out, "usage: 705 fresh") || !strings.Contains(out, "input 5 · cache write 0 · output 700  cache read 95.0k") {
		t.Errorf("the fork counts its parent's usage:\n%s", out)
	}
	defer func() {
		ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1", "--discard"))
		if exists(base) {
			t.Error("the base file of a finished task is left")
		}
	}()
	for _, c := range []struct {
		env  []string
		args []string
		want string
	}{
		{nil, nil, "CLAUDE_CODE_SESSION_ID is not set"},
		{[]string{"CLAUDE_CODE_SESSION_ID=abcd1234"}, nil, "needs the full session id"},
		{[]string{"CODEX_THREAD_ID=" + parentThread}, nil, "which is a codex session, and a fork stays in its engine: use --engine codex"},
		{mine(), []string{"--engine", "codex"}, "which is a claude session, and a fork stays in its engine: use --engine claude"},
		{mine(), []string{"--after", "T1"}, "cannot wait --after"},
	} {
		h.ResetArgv()
		args := append([]string{"launch", "--title", "x", "--cwd", h.Root, "--fork"}, c.args...)
		r := csq(t, h, opts{stdin: "p", env: c.env}, args...)
		if r.code != 1 || !strings.Contains(r.stderr, c.want) {
			t.Errorf("%v %v: %+v", c.env, c.args, r)
		}
		if len(h.Argv()) != 0 {
			t.Errorf("%v: claude ran: %q", c.env, h.Argv())
		}
	}
}

// A Codex fork is thread/fork of the dispatching thread; developer
// instructions are ignored there, so the worker rules lead the first turn.
func TestCodexLaunchFork(t *testing.T) {
	h, srv, repo := codexHome(t)
	srv.Seed(parentThread)
	r := ok(t, csq(t, h, opts{stdin: "continue the refactor", env: fromCodex()}, "launch", "--title", "fork job", "--engine", "codex", "--cwd", repo, "--fork", "--", "-m", "gpt-x"))
	id := launchedCodexRe.FindStringSubmatch(r.stdout)[3]
	fork := srv.Last("thread/fork")
	if fork["threadId"] != parentThread || fork["cwd"] != repo || fork["model"] != "gpt-x" || fork["developerInstructions"] != nil || fork["sandbox"] != "danger-full-access" {
		t.Errorf("thread/fork %v", fork)
	}
	if srv.Last("thread/start") != nil {
		t.Error("a fork started a new thread")
	}
	th := srv.Thread(id)
	if th == nil || th.Name != "T1 · fork job" || len(th.Turns) != 2 {
		t.Fatalf("thread %+v", th)
	}
	b, _ := json.Marshal(srv.Last("turn/start")["input"])
	var in []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &in) != nil || len(in) != 1 {
		t.Fatalf("input %s", b)
	}
	if text := in[0].Text; !strings.Contains(text, "-C REPO worktree add") || !strings.Contains(text, "Your task ID is T1.") || !strings.HasSuffix(text, "\n---\n\ncontinue the refactor") {
		t.Errorf("first turn %q", text)
	}
}
