package squad

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNativePeerEnvelopeAndCrossEngineHeader(t *testing.T) {
	s := &State{ID: "demo", Members: map[string]*Member{"master": {ID: "master", Peer: "/tmp/master.sock"}}}
	msg := &Message{ID: "M18", From: "master", To: "worker", Task: "T2", Text: "Check the parser.\nKeep <T> intact; quote </cross-session-message> literally."}
	frame := claudePeerFrame(s, msg, 3)
	content := frame["message"].(map[string]string)["content"]
	if !strings.HasPrefix(content, `<cross-session-message from="uds:`) || !strings.Contains(content, `from-name="master"`) || strings.Count(content, "</cross-session-message>") != 1 {
		t.Fatalf("invalid native envelope: %s", content)
	}
	if !strings.Contains(content, "message_id=M18 task_id=T2 recipient_generation=3") || !strings.Contains(content, "<T>") {
		t.Fatal("lost message metadata or code")
	}
	delete(s.Members, "master")
	frame = claudePeerFrame(s, msg, 3)
	if frame["from"] != "did:csquad:demo:master" {
		t.Fatal("cross-engine sender identity missing")
	}
	body := messageBody(msg, 3, true)
	if !strings.Contains(body, "from=master") || !strings.HasSuffix(body, msg.Text) {
		t.Fatal("Codex sender or body missing")
	}
	encoded, err := json.Marshal(frame)
	must(t, err)
	if strings.Contains(string(encoded), "not human permission") || strings.Contains(string(encoded), "Acknowledge via CLI") {
		t.Fatal("per-message instructions returned")
	}
}

func TestMessagingContextInjectedOnceAndOnResume(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Executable = "/missing/versioned csquad"
		return nil
	}))
	for _, step := range []struct {
		event, session string
		want           bool
	}{
		{"UserPromptSubmit", "first", true},
		{"UserPromptSubmit", "first", false},
		{"PreToolUse", "first", false},
		{"SessionStart", "first", true},
		{"UserPromptSubmit", "resumed", true},
	} {
		f, err := os.CreateTemp(t.TempDir(), "hook-output")
		must(t, err)
		original := os.Stdout
		os.Stdout = f
		raw, _ := json.Marshal(map[string]string{"hook_event_name": step.event, "session_id": step.session, "tool_name": "Bash"})
		err = hookInput(st, "a", 1, strings.NewReader(string(raw)))
		os.Stdout = original
		must(t, err)
		_, err = f.Seek(0, 0)
		must(t, err)
		out, err := io.ReadAll(f)
		must(t, err)
		must(t, f.Close())
		if step.want {
			for _, want := range []string{"csquad COMMAND", "bound to this session", "non-login shell", "Codex exec_command, set login:false", "without an absolute executable path or manual PATH overrides", "Developers and reviewers", "not message quotas", "ordinary uncertainty is not by itself a reason to stop"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("runtime context missing %q: %s", want, out)
				}
			}
			if strings.Contains(string(out), "/missing/versioned csquad") {
				t.Fatalf("runtime context requires absolute command: %s", out)
			}
		}
		if strings.Contains(string(out), "C-Squad coordinates a team") != step.want {
			t.Fatalf("unexpected context injection on %s: %s", step.event, out)
		}
	}
}
