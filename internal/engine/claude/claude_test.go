package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.MaybeFakeClaude()
	os.Exit(m.Run())
}

func newEngine(h *testutil.Home, env ...string) *Engine {
	return New(engine.Config{Home: h.Csquad, ClaudeDir: h.Claude, Env: h.Env(env...)})
}

func TestRawStateFollowsAgentView(t *testing.T) {
	for _, c := range []struct {
		s    Session
		want string
	}{
		{Session{State: "done", Status: "busy"}, "done"},
		{Session{State: "done", Status: "idle"}, "done"},
		// A message held for the user's approval after the turn ended, as
		// claude agents --json --all lists it (Claude Code 2.1.295).
		{Session{State: "done", Status: "waiting", WaitingFor: json.RawMessage(`"permission prompt"`)}, "needs_input"},
		{Session{State: "done", Status: "idle", WaitingFor: json.RawMessage(`"permission prompt"`)}, "needs_input"},
		{Session{State: "stopped", Status: "waiting"}, "needs_input"},
		{Session{State: "done", Status: "idle", WaitingFor: json.RawMessage(`null`)}, "done"},
		{Session{State: "working", Status: "busy", WaitingFor: json.RawMessage(`"permission prompt"`)}, "working"},
		{Session{State: "crashed"}, "failed"},
		{Session{State: "stopped", Status: "busy"}, "stopped"},
		{Session{State: "working", Status: "busy"}, "working"},
		{Session{State: "working", Status: "shell"}, "working"},
		{Session{State: "working", Status: "waiting"}, "needs_input"},
		{Session{State: "working", Status: "idle", Tempo: "blocked"}, "needs_input"},
		{Session{State: "working", Status: "idle", WaitingFor: json.RawMessage(`"permission"`)}, "needs_input"},
		{Session{State: "blocked", Status: "idle"}, "needs_input"},
		{Session{State: "working", Status: "idle"}, "idle"},
		{Session{State: "working"}, "working"},
		{Session{}, "unknown"},
	} {
		if got := RawState(c.s); got != c.want {
			t.Errorf("%+v: %s, want %s", c.s, got, c.want)
		}
	}
}

func TestStatesMatchShortAndFullIDs(t *testing.T) {
	list := []Session{{ID: "1b8dd2ca", SessionID: "1b8dd2ca-b5b7-4bb6-aee8-7f8ae9206ff9", State: "done", Cwd: "/w"}}
	got := states(list, []string{"1b8dd2ca", "1b8dd2ca-b5b7-4bb6-aee8-7f8ae9206ff9", "deadbeef"})
	if got["1b8dd2ca"].State != "done" || got["1b8dd2ca"].SessionID != list[0].SessionID || got["1b8dd2ca-b5b7-4bb6-aee8-7f8ae9206ff9"].Cwd != "/w" || got["deadbeef"].State != engine.Gone {
		t.Errorf("%+v", got)
	}
}

func TestCheckArgs(t *testing.T) {
	h := testutil.NewHome(t)
	e := newEngine(h)
	os.WriteFile(filepath.Join(h.Root, "s.json"), []byte(`{"model":"opus"}`), 0o644)
	got, err := e.CheckArgs([]string{"--effort", "high", "--settings", "s.json"}, h.Root)
	if err != nil || got[3] != filepath.Join(h.Root, "s.json") {
		t.Errorf("%q %v", got, err)
	}
	for _, bad := range [][]string{{"--settings"}, {"--settings", "missing.json"}, {"--settings", "[1]"}, {"--settings={"}, {"--name=x"}, {"--fork-session"}} {
		if _, err := e.CheckArgs(bad, h.Root); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMessagesFoldTools(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"},{"type":"tool_use","name":"Bash"},{"type":"tool_use","name":"Edit"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"user","message":{"content":"<command-name>/x</command-name>"}}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`not json`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done <system-reminder>secret</system-reminder>"}]}}`,
	}, "\n")
	got := Messages(strings.NewReader(in))
	if len(got) != 3 || got[0].Text != "hi" || got[1].Role != "tools" || got[1].Tools[0] != (engine.ToolCount{Name: "Bash", Count: 2}) || got[1].Tools[1].Name != "Edit" || got[2].Text != "done" {
		t.Errorf("%+v", got)
	}
}

// The inbox wire: an auth line with the peer token from the key file, then
// one user frame naming the session, newline-delimited, then close.
func TestSendDeliversToTheInbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket path length on Windows CI")
	}
	h := testutil.NewHome(t)
	full := "aaaa0001-0000-4000-8000-000000000000"
	h.SetSessions([]testutil.Session{{ID: "aaaa0001", SessionID: full, Kind: "background", Name: "T1 · x", State: "working", Status: "idle", Cwd: h.Root}})
	sockDir, _ := os.MkdirTemp("", "cs") // short: unix socket paths are limited
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "1.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan []string, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var lines []string
			sc := bufio.NewScanner(c)
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			c.Close()
			got <- lines
		}
	}()
	sessions := filepath.Join(h.Claude, "sessions")
	os.MkdirAll(sessions, 0o700)
	b, _ := json.Marshal(map[string]any{"pid": 4242, "sessionId": full, "messagingSocketPath": sock, "kind": "bg"})
	os.WriteFile(filepath.Join(sessions, "4242.json"), b, 0o600)
	e := newEngine(h)
	keyHash := hashOf(sock)
	os.WriteFile(filepath.Join(sessions, "4242."+keyHash+".key"), []byte(`{"peerToken":"0123456789abcdef0123456789abcdef"}`), 0o600)
	how, err := e.Send(context.Background(), "aaaa0001", "also handle\nempty input")
	if err != nil || !strings.Contains(how, "inbox") {
		t.Fatalf("%q %v", how, err)
	}
	var lines []string
	for {
		select {
		case lines = <-got:
		case <-time.After(5 * time.Second):
			t.Fatal("nothing delivered")
		}
		if len(lines) > 0 {
			break // the liveness probe connects without writing
		}
	}
	if len(lines) != 2 || lines[0] != `{"token":"0123456789abcdef0123456789abcdef","type":"auth"}` {
		t.Fatalf("%q", lines)
	}
	var frame struct {
		Type    string `json:"type"`
		Session string `json:"session_id"`
		Message struct {
			Role, Content string
		} `json:"message"`
	}
	json.Unmarshal([]byte(lines[1]), &frame)
	if frame.Type != "user" || frame.Session != full || frame.Message.Content != "also handle\nempty input" || frame.Message.Role != "user" {
		t.Errorf("%s", lines[1])
	}
	// No live socket: woken with claude --bg --resume.
	l.Close()
	how, err = e.Send(context.Background(), "aaaa0001", "next")
	if err != nil || !strings.Contains(how, "woke") || testutil.Arg(h.LastArgv(), "--resume") != full {
		t.Errorf("%q %v %q", how, err, h.LastArgv())
	}
}

func TestStartFailsCleanly(t *testing.T) {
	h := testutil.NewHome(t)
	e := newEngine(h)
	_, err := e.Start(context.Background(), engine.StartSpec{ID: 3, Title: "FAILME", Cwd: h.Root, Prompt: strings.Repeat("p", MaxInlinePrompt+1)})
	if err == nil || !strings.Contains(err.Error(), "launching T3 · FAILME failed:\nworkspace not trusted") {
		t.Errorf("%v", err)
	}
	for _, f := range []string{SettingsFile(h.Csquad, 3), PromptFile(h.Csquad, 3)} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s left", f)
		}
	}
}
