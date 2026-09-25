package squad

import (
	"fmt"
	"strings"
	"testing"

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
		{"other draft", "› unrelated text [C-Squad startup M8]\nmodel · project", false},
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
