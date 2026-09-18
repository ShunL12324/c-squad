package squad

import (
	"testing"

	"github.com/ShunL12324/c-squad/internal/process"
)

func TestRestartPreservesConversationAndFencesOldDelivery(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		m := s.Members["a"]
		m.EngineID = "native-conversation"
		m.Instructions = "review changes"
		m.Env = map[string]string{"CODEX_HOME": "/account-two"}
		m.Pane, m.Peer = "%old", "/old.sock"
		m.RunnerPID, m.EnginePID = 111, 222
		m.Processes = []process.Identity{{PID: 222, Start: "old"}}
		msg := s.message("master", "a", "", "continue review", "")
		msg.State, msg.Error, msg.Attempt = DeliveryStateNeedsAttention, "old transport error", "old-attempt"
		msg.Attempts, msg.RecipientGeneration = 3, m.Generation
		m.Generation++
		m.resetRuntime()
		msg.resetDelivery()
		return nil
	}))
	s, err := st.read()
	must(t, err)
	m := s.Members["a"]
	if m.Generation != 2 || m.EngineID != "native-conversation" || m.Instructions != "review changes" || m.Env["CODEX_HOME"] != "/account-two" {
		t.Fatal("restart lost durable identity/configuration")
	}
	if m.Pane != "" || m.Peer != "" || m.RunnerPID != 0 || m.EnginePID != 0 || len(m.Processes) != 0 {
		t.Fatal("restart retained old process/transport identity")
	}
	msg := s.Messages[0]
	if msg.State != DeliveryStatePending || msg.Error != "" || msg.Attempts != 0 || msg.Attempt != "" || msg.RecipientGeneration != 0 || msg.Text != "continue review" {
		t.Fatal("restart did not preserve message content and reset its delivery lease")
	}
}
