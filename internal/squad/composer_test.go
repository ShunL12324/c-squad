package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

func TestCodexComposerAfterResume(t *testing.T) {
	for _, tt := range []struct {
		name, pane string
		ready      bool
	}{
		{"resumed without banner", "Previous result\n› Ask Codex to do anything\nmodel · project", true},
		{"typed input", "› Please inspect this code\nmodel · project", false},
		{"working", "Working (esc to interrupt)\n› Ask Codex to do anything\nmodel", false},
		{"historical prompt", "› Ask Codex to do anything\n" + strings.Repeat("old output\n", 20), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if codexEmptyComposer(tt.pane) != tt.ready {
				t.Fatal("incorrect composer readiness")
			}
		})
	}
}

func TestCodexBootstrapDraftRequiresCurrentComposer(t *testing.T) {
	marker := codexBootstrapMarker("M7")
	for _, tt := range []struct {
		name, pane string
		want       bool
	}{
		{"own draft", "Earlier output\n› [C-Squad message_id=M7] assignment " + marker + "\nmodel · project", true},
		{"wrapped own draft", "› [C-Squad message_id=M7] assignment\n  more text " + marker + "\nmodel · project", true},
		{"marker split inside a word", "› [C-Squad message_id=M7] assignment\n[C-Squad start\nup M7]\nmodel · project", true},
		{"long draft scrolls prompt glyph away", "input continuation\n" + strings.Repeat("line of assignment\n", 20) + marker + "\nmodel · project", false},
		{"other draft", "› unrelated text [C-Squad startup M8]\nmodel · project", false},
		{"historical marker without composer", "old " + marker + "\nResuming session…", false},
		{"historical prompt with output", "› assignment " + marker + "\nResuming session…", false},
		{"marker only in old output", "› assignment " + marker + "\n" + strings.Repeat("old output\n", 15) + "› Ask Codex to do anything\nmodel · project", false},
		{"old prompt above empty composer", "› assignment " + marker + "\nanswer\n› Ask Codex to do anything\nmodel · project", false},
		{"working", "Working (esc to interrupt)\n› assignment " + marker + "\nmodel · project", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexOwnDraft(tt.pane, marker); got != tt.want {
				t.Fatalf("own draft = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodexBootstrapNeedsMatchingNativePromptHook(t *testing.T) {
	st := testStore(t)
	var id string
	must(t, st.update(func(s *State) error {
		s.Members["a"].Engine = config.Codex
		msg := s.message("master", "a", "", "assignment", "")
		msg.State = DeliveryStatePending
		msg.BootstrapGeneration = 1
		msg.BootstrapTyped = true
		id = msg.ID
		return nil
	}))
	for _, prompt := range []string{
		"unrelated user prompt",
		"[C-Squad message_id=" + id + " from=master recipient_generation=1] assignment [C-Squad startup M999]",
	} {
		must(t, hookInput(st, "a", 1, strings.NewReader(fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"thread","prompt":%q}`, prompt))))
		s, err := st.read()
		must(t, err)
		if s.Messages[0].State != DeliveryStatePending {
			t.Fatal("unrelated prompt acknowledged bootstrap")
		}
	}
	prompt := "[C-Squad message_id=" + id + " from=master recipient_generation=1] assignment " + codexBootstrapMarker(id)
	must(t, hookInput(st, "a", 1, strings.NewReader(fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"thread","prompt":%q}`, prompt))))
	s, err := st.read()
	must(t, err)
	if s.Messages[0].State != DeliveryStateSent || s.Messages[0].Error != "" {
		t.Fatalf("matching native prompt was not acknowledged: %+v", s.Messages[0])
	}
}

func TestCodexBootstrapHoldsLaterMessagesUntilPromptAck(t *testing.T) {
	st := testStore(t)
	var second string
	must(t, st.update(func(s *State) error {
		s.Members["a"].Engine = config.Codex
		first := s.message("master", "a", "", "first", "")
		first.BootstrapGeneration = s.Members["a"].Generation
		first.BootstrapTyped = true
		second = s.message("master", "a", "", "second", "").ID
		return nil
	}))
	must(t, st.deliver(second))
	s, err := st.read()
	must(t, err)
	if s.Messages[1].State != DeliveryStatePending || s.Messages[1].Attempts != 0 || !strings.Contains(s.Messages[1].Error, "earlier Codex startup") {
		t.Fatalf("later message bypassed unacknowledged first prompt: %+v", s.Messages[1])
	}
}

func TestCodexBootstrapTransportRetriesOnlyEnterUntilNativeAck(t *testing.T) {
	st := testStore(t)
	dir := t.TempDir()
	pane := filepath.Join(dir, "pane")
	log := filepath.Join(dir, "tmux.log")
	bin := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
shift 2
case "$1" in
list-clients) printf '%s' "$BOOTSTRAP_CLIENT" ;;
capture-pane) cat "$BOOTSTRAP_PANE" ;;
send-keys)
  printf '%s\n' "$*" >> "$BOOTSTRAP_LOG"
  case " $* " in
    *" -l "*) printf '› %s\nmodel · project\n' "$BOOTSTRAP_DRAFT" > "$BOOTSTRAP_PANE" ;;
  esac ;;
*) exit 1 ;;
esac
`
	must(t, os.WriteFile(bin, []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BOOTSTRAP_PANE", pane)
	t.Setenv("BOOTSTRAP_LOG", log)
	var id, draft string
	must(t, st.update(func(s *State) error {
		m := s.Members["a"]
		m.Engine, m.Session, m.Pane = config.Codex, "member-session", "%1"
		msg := s.message("master", "a", "", "assignment", "")
		id = msg.ID
		draft = strings.ReplaceAll(messageBody(msg, m.Generation, true), "\n", " ") + " " + codexBootstrapMarker(id)
		return nil
	}))
	t.Setenv("BOOTSTRAP_DRAFT", draft)
	must(t, os.WriteFile(pane, []byte("› Ask Codex to do anything\nmodel · project\n"), 0600))
	must(t, st.deliver(id))
	state, err := st.read()
	must(t, err)
	if msg := state.Messages[0]; msg.State != DeliveryStatePending || !msg.BootstrapTyped || !strings.Contains(msg.Error, "awaiting native") {
		t.Fatalf("tmux success acknowledged unsubmitted draft: %+v", msg)
	}
	calls := func() string { b, e := os.ReadFile(log); must(t, e); return string(b) }
	if got := calls(); strings.Count(got, " -l ") != 1 || strings.Count(got, " Enter") != 1 {
		t.Fatalf("initial transport calls: %q", got)
	}
	retry := func() {
		must(t, st.update(func(s *State) error {
			s.Messages[0].Attempt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
			return nil
		}))
	}
	retry()
	must(t, st.deliver(id))
	if got := calls(); strings.Count(got, " -l ") != 1 || strings.Count(got, " Enter") != 2 {
		t.Fatalf("retry repasted or skipped Enter: %q", got)
	}
	before := calls()
	for _, mode := range []string{"foreign", "attached", "working"} {
		t.Run(mode, func(t *testing.T) {
			retry()
			switch mode {
			case "foreign":
				must(t, os.WriteFile(pane, []byte("› another prompt\nmodel · project\n"), 0600))
			case "attached":
				t.Setenv("BOOTSTRAP_CLIENT", "member-session")
			case "working":
				must(t, st.update(func(s *State) error { s.Members["a"].State = MemberStateWorking; return nil }))
			}
			if err := st.deliver(id); err == nil {
				t.Fatal("unsafe composer accepted")
			}
			if got := calls(); got != before {
				t.Fatalf("unsafe composer received keys: %q", got)
			}
		})
		t.Setenv("BOOTSTRAP_CLIENT", "")
		must(t, os.WriteFile(pane, []byte("› "+draft+"\nmodel · project\n"), 0600))
		must(t, st.update(func(s *State) error { s.Members["a"].State = MemberStateIdle; return nil }))
	}
	prompt := draft
	must(t, hookInput(st, "a", 1, strings.NewReader(fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"thread","prompt":%q}`, prompt))))
	state, err = st.read()
	must(t, err)
	if state.Messages[0].State != DeliveryStateSent {
		t.Fatalf("matching native hook did not acknowledge startup: %+v", state.Messages[0])
	}
}
