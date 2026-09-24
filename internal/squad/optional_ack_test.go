package squad

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// needs_attention was removed, but DeliveryState is a string: an old ledger still
// unmarshals cleanly and such a message would then match neither the pending nor the
// terminal branch of deliver, staying forever undelivered and never cleared. The
// version bump must normalize it once, on load, and persist the result.
func TestLegacyNeedsAttentionLedgerIsNormalizedOnce(t *testing.T) {
	const legacyACK = "Transport succeeded but no agent ACK; inspect inbox/recipient or explicitly message retry (no automatic reinjection)"
	st := testStore(t)
	legacy := `{"version":2,"id":"test","active":true,"members":{"a":{"id":"a","engine":"claude","state":"idle","generation":1}},` +
		`"tasks":{},"questions":{},"messages":[` +
		`{"id":"M1","from":"master","to":"a","text":"transported","state":"needs_attention","error":"` + legacyACK + `","attempts":3,"attempt":"original","recipient_generation":4},` +
		`{"id":"M2","from":"master","to":"a","text":"never arrived","state":"needs_attention","error":"unknown transport failure","attempts":2,"attempt":"original"}],"sequence":2}`
	_, err := st.DB.Exec("INSERT INTO state(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", legacy)
	must(t, err)

	s, err := st.read()
	must(t, err)
	if s.Version != stateVersion {
		t.Fatalf("version = %d, want %d", s.Version, stateVersion)
	}
	if m := s.Messages[0]; m.State != DeliveryStateSent || m.Error != "" || m.Attempts != 3 {
		t.Fatalf("transported message lost its outcome: %+v", m)
	}
	if m := s.Messages[1]; m.State != DeliveryStatePending || m.Error != "" || m.Attempts != 0 {
		t.Fatalf("unfinished message was not requeued: %+v", m)
	}

	// The first write persists the upgrade; nothing may re-enter the ledger as
	// needs_attention, and a second load must change nothing.
	must(t, st.update(func(*State) error { return nil }))
	var raw string
	must(t, st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw))
	var persisted struct {
		Version int `json:"version"`
	}
	must(t, json.Unmarshal([]byte(raw), &persisted))
	if strings.Contains(raw, "needs_attention") || persisted.Version != stateVersion {
		t.Fatalf("upgrade was not written back: %s", raw)
	}
	again, err := st.read()
	must(t, err)
	if again.Messages[0].State != DeliveryStateSent || again.Messages[1].State != DeliveryStatePending || again.Version != stateVersion {
		t.Fatal("reload was not idempotent")
	}
}

// The upgrade is gated on the ledger version, so the runtime must not compare
// message text on every read and update the way the old migration did.
func TestNormalizeStateDoesNotRewriteMessagesAfterUpgrade(t *testing.T) {
	s := &State{Version: stateVersion, Messages: []*Message{{ID: "M1", State: "needs_attention", Error: "unknown transport failure", Attempts: 2}}}
	normalizeState(s)
	if s.Messages[0].State != "needs_attention" || s.Messages[0].Attempts != 2 {
		t.Fatal("normalizeState still rewrites delivery records on the hot path")
	}
}

func TestAutomaticRecoveryAndExplicitRetryAreDifferent(t *testing.T) {
	for _, state := range []DeliveryState{DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded, DeliveryStatePending, DeliveryStateSending} {
		m := &Message{State: state, Attempts: 2, Attempt: "original"}
		m.recoverDelivery()
		switch state {
		case DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded:
			if m.State != state || m.Attempts != 2 {
				t.Fatalf("%s was requeued", state)
			}
		default:
			if m.State != DeliveryStatePending || m.Attempts != 0 {
				t.Fatalf("%s cannot retry", state)
			}
		}
	}
	m := &Message{State: DeliveryStateSent, Attempts: 1}
	m.resetDelivery() // Explicit re-request of the same message.
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
