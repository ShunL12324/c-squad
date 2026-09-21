package squad

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestLegacyACKWarningMigrationIsConservative(t *testing.T) {
	for _, diagnostic := range []string{
		"Transport succeeded but no agent ACK; inspect inbox/recipient or explicitly message retry (no automatic reinjection)",
		"No agent ACK after 3 delivery attempts; inspect recipient or restart/requeue",
	} {
		s := &State{Messages: []*Message{{ID: "M1", State: DeliveryStateNeedsAttention, Error: diagnostic, Attempts: 3, Attempt: "original", RecipientGeneration: 4}}}
		normalizeState(s)
		m := s.Messages[0]
		if m.State != DeliveryStateSent || m.Error != "" || !strings.Contains(m.DeliveryNote, diagnostic) || m.Attempts != 3 || m.Attempt != "original" || m.RecipientGeneration != 4 {
			t.Fatalf("migration lost transport history: %+v", m)
		}
		m.recoverDelivery()
		if m.State != DeliveryStateSent || m.Attempts != 3 {
			t.Fatal("migrated success requeued")
		}
	}
	m := &Message{State: DeliveryStateNeedsAttention, Error: "unknown transport failure", Attempts: 2}
	m.migrateLegacyACKWarning()
	if m.State != DeliveryStateNeedsAttention || m.Error != "unknown transport failure" || m.DeliveryNote != "" {
		t.Fatal("unknown diagnostic was interpreted as success")
	}
}

func TestAutomaticRecoveryAndExplicitRetryAreDifferent(t *testing.T) {
	for _, state := range []DeliveryState{DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded, DeliveryStatePending, DeliveryStateSending, DeliveryStateNeedsAttention} {
		m := &Message{State: state, Attempts: 2, Attempt: "original", RecipientGeneration: 1}
		m.recoverDelivery()
		switch state {
		case DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded:
			if m.State != state || m.Attempts != 2 || m.RecipientGeneration != 1 {
				t.Fatalf("%s was requeued", state)
			}
		default:
			if m.State != DeliveryStatePending || m.Attempts != 0 || m.RecipientGeneration != 0 {
				t.Fatalf("%s cannot retry", state)
			}
		}
	}
	m := &Message{State: DeliveryStateSent, Attempts: 1}
	m.resetDelivery() // Explicit user re-request, as used by Brief report.
	if m.State != DeliveryStatePending || m.Attempts != 0 {
		t.Fatal("explicit re-request disabled")
	}
}

func TestInboxRetainsSentWithoutClaimingReadOrCompletion(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.message("master", "a", "", "delivered", "").State = DeliveryStateSent
		s.message("master", "a", "", "hidden", "").State = DeliveryStateAcknowledged
		return nil
	}))
	f, err := os.CreateTemp(t.TempDir(), "inbox")
	must(t, err)
	prior := os.Stdout
	os.Stdout = f
	err = messageCommand(st, "a", []string{"message", "inbox"}, options{})
	os.Stdout = prior
	must(t, err)
	_, err = f.Seek(0, 0)
	must(t, err)
	raw, err := io.ReadAll(f)
	must(t, err)
	must(t, f.Close())
	var messages []*Message
	must(t, json.Unmarshal(raw, &messages))
	if len(messages) != 1 || messages[0].State != DeliveryStateSent {
		t.Fatalf("wrong inbox records: %s", raw)
	}
	s, err := st.read()
	must(t, err)
	if s.Messages[0].State != DeliveryStateSent {
		t.Fatal("inbox silently acknowledged delivery")
	}
}
