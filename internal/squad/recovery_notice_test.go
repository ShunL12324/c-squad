package squad

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recoveryStore gives member "a" a reachable inbox and one open task it owns.
func recoveryStore(t *testing.T) (*Store, string) {
	t.Helper()
	st := testStore(t)
	sock := filepath.Join(t.TempDir(), "inbox.sock")
	must(t, st.update(func(s *State) error {
		m := s.Members["a"]
		m.EngineID = "test"
		m.Peer = sock
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a", "b"}}
		return nil
	}))
	return st, sock
}

// inbox accepts connections on sock and reports every frame it receives.
func inbox(t *testing.T, sock string) chan string {
	t.Helper()
	l, e := net.Listen("unix", sock)
	must(t, e)
	t.Cleanup(func() { l.Close() })
	received := make(chan string, 8)
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			var v map[string]any
			json.NewDecoder(c).Decode(&v)
			c.Close()
			b, _ := json.Marshal(v)
			received <- string(b)
		}
	}()
	return received
}

func recoveryNotices(t *testing.T, st *Store) []*Message {
	t.Helper()
	s, e := st.read()
	must(t, e)
	var found []*Message
	for _, m := range s.Messages {
		if m.RequestKey == "master:recovery:T1" {
			found = append(found, m)
		}
	}
	return found
}

// Every failed resume replays the whole recovery block. Repeating it must not
// accumulate wake-ups for a notification the owner has not received yet.
func TestRecoveryNoticeIsIdempotentAcrossFailedResumes(t *testing.T) {
	st, sock := recoveryStore(t)
	received := inbox(t, sock)
	for range 3 {
		must(t, st.update(func(s *State) error {
			// A resume attempt bumps generations and recovers interrupted transport
			// before it queues notifications.
			s.Members["a"].Generation++
			for _, m := range s.Messages {
				m.recoverDelivery()
			}
			s.queueRecoveryNotices()
			return nil
		}))
	}
	notices := recoveryNotices(t, st)
	if len(notices) != 1 {
		t.Fatalf("three failed resumes queued %d notices", len(notices))
	}
	if notices[0].To != "a" || notices[0].State != DeliveryStatePending {
		t.Fatalf("unexpected notice: %+v", notices[0])
	}
	must(t, st.syncMessages())
	select {
	case v := <-received:
		if !strings.Contains(v, notices[0].ID) {
			t.Fatalf("wrong message delivered: %s", v)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery notice not delivered")
	}
	s, e := st.read()
	must(t, e)
	if got := recoveryNotices(t, st); len(got) != 1 || got[0].ID != notices[0].ID {
		t.Fatal("delivery changed notice identity")
	}
	if s.Messages[0].RecipientGeneration != s.Members["a"].Generation {
		t.Fatalf("stale recipient generation %d, member is at %d", s.Messages[0].RecipientGeneration, s.Members["a"].Generation)
	}
}

// A task finished between enqueue and delivery no longer needs its owner.
// The notification is retained for history and never transported.
func TestRecoveryNoticeSupersededWhenTaskCompletes(t *testing.T) {
	st, sock := recoveryStore(t)
	received := inbox(t, sock)
	must(t, st.update(func(s *State) error { s.queueRecoveryNotices(); return nil }))
	id := recoveryNotices(t, st)[0].ID
	must(t, st.update(func(s *State) error { s.Tasks["T1"].State = TaskPhaseDone; return nil }))
	must(t, st.deliver(id))
	s, e := st.read()
	must(t, e)
	if s.Messages[0].State != DeliveryStateSuperseded {
		t.Fatalf("stale notice is %s", s.Messages[0].State)
	}
	if s.Messages[0].Attempts != 0 {
		t.Fatalf("stale notice attempted transport %d times", s.Messages[0].Attempts)
	}
	select {
	case v := <-received:
		t.Fatalf("stale notice woke the recipient: %s", v)
	case <-time.After(200 * time.Millisecond):
	}
}

// Suppression must not reach past undelivered notifications: once the owner has
// seen one, a later interruption is real news and deserves its own wake-up.
func TestDeliveredRecoveryNoticeDoesNotSuppressNewInterruption(t *testing.T) {
	st, sock := recoveryStore(t)
	received := inbox(t, sock)
	must(t, st.update(func(s *State) error { s.queueRecoveryNotices(); return nil }))
	first := recoveryNotices(t, st)[0].ID
	must(t, st.syncMessages())
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("first notice not delivered")
	}
	if got := recoveryNotices(t, st); got[0].State != DeliveryStateSent {
		t.Fatalf("first notice is %s", got[0].State)
	}
	must(t, st.update(func(s *State) error { s.queueRecoveryNotices(); return nil }))
	notices := recoveryNotices(t, st)
	if len(notices) != 2 {
		t.Fatalf("new interruption produced %d notices", len(notices))
	}
	if notices[1].ID == first || notices[1].State != DeliveryStatePending {
		t.Fatalf("second interruption was swallowed: %+v", notices[1])
	}
	must(t, st.syncMessages())
	select {
	case v := <-received:
		if !strings.Contains(v, notices[1].ID) {
			t.Fatalf("wrong message delivered: %s", v)
		}
	case <-time.After(time.Second):
		t.Fatal("second notice not delivered")
	}
}

// Expiry is bound to recovery notifications alone. A question queued before a
// restart is still owed to its recipient afterwards, however old it is.
func TestOrdinaryPeerMessageSurvivesRestart(t *testing.T) {
	st, sock := recoveryStore(t)
	received := inbox(t, sock)
	var id string
	must(t, st.update(func(s *State) error {
		id = s.message("b", "a", "T1", "did you already push the fix?", "").ID
		return nil
	}))
	must(t, st.update(func(s *State) error {
		s.Members["a"].Generation += 2
		for _, m := range s.Messages {
			m.recoverDelivery()
		}
		s.Tasks["T1"].State = TaskPhaseDone
		s.queueRecoveryNotices()
		return nil
	}))
	must(t, st.syncMessages())
	select {
	case v := <-received:
		if !strings.Contains(v, id) {
			t.Fatalf("wrong message delivered: %s", v)
		}
	case <-time.After(time.Second):
		t.Fatal("peer message queued before the restart was dropped")
	}
	s, e := st.read()
	must(t, e)
	if s.Messages[0].State != DeliveryStateSent {
		t.Fatalf("peer message is %s", s.Messages[0].State)
	}
}

// A crash between enqueue and transport leaves a claimed notification behind.
// Recovery resets it, and the owner is woken exactly once.
func TestRecoveryNoticeDeliveredExactlyOnceAcrossCrash(t *testing.T) {
	st, sock := recoveryStore(t)
	received := inbox(t, sock)
	must(t, st.update(func(s *State) error { s.queueRecoveryNotices(); return nil }))
	must(t, st.update(func(s *State) error {
		// The process died while transporting: the claim outlives it.
		s.Messages[0].State = DeliveryStateSending
		s.Messages[0].Attempt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
		s.Messages[0].Attempts = 1
		return nil
	}))
	must(t, st.update(func(s *State) error {
		s.Members["a"].Generation++
		for _, m := range s.Messages {
			m.recoverDelivery()
		}
		s.queueRecoveryNotices()
		return nil
	}))
	if notices := recoveryNotices(t, st); len(notices) != 1 {
		t.Fatalf("interrupted transport produced %d notices", len(notices))
	}
	must(t, st.syncMessages())
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("recovered notice not delivered")
	}
	must(t, st.syncMessages())
	select {
	case v := <-received:
		t.Fatalf("notice delivered twice: %s", v)
	case <-time.After(200 * time.Millisecond):
	}
	s, e := st.read()
	must(t, e)
	if len(s.Messages) != 1 || s.Messages[0].State != DeliveryStateSent {
		t.Fatalf("unexpected ledger: %+v", s.Messages)
	}
}
