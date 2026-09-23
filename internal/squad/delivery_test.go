package squad

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
)

// peerInbox accepts the native local frames deliver writes and counts them.
func peerInbox(t *testing.T, received *atomic.Int32) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "peer.sock")
	l, err := net.Listen("unix", path)
	must(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			_, _ = io.Copy(io.Discard, c)
			received.Add(1)
			_ = c.Close()
		}
	}()
	return path
}

func registeredRecipient(t *testing.T, st *Store, id, peer string) string {
	t.Helper()
	var message string
	must(t, st.update(func(s *State) error {
		m := s.Members[id]
		m.EngineID, m.Peer = "native-conversation", peer
		message = s.message("master", id, "", "for the incarnation that was running", "").ID
		return nil
	}))
	return message
}

func waitForState(t *testing.T, st *Store, id string, want DeliveryState) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		s, err := st.read()
		must(t, err)
		for _, m := range s.Messages {
			if m.ID == id && m.State == want {
				return
			}
		}
	}
	t.Fatalf("%s never reached %s", id, want)
}

// The delivery fence is the recipient generation captured while claiming the
// message, not a stored field: a member that restarts after the claim must not
// receive the frame its previous incarnation was owed.
func TestDeliveryFencesMemberRestartBeforeTransport(t *testing.T) {
	var received atomic.Int32
	st := testStore(t)
	id := registeredRecipient(t, st, "a", peerInbox(t, &received))
	// Holding the member lock places the restart between the claim and the fence
	// comparison deliver makes once it takes that lock.
	unlock, err := filelock.Acquire(st.Dir, "member-a", false)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- st.deliver(id) }()
	waitForState(t, st, id, DeliveryStateSending)
	must(t, st.update(func(s *State) error { s.Members["a"].Generation++; return nil }))
	unlock()
	must(t, <-done)
	if received.Load() != 0 {
		t.Fatal("frame reached the restarted member")
	}
	s, err := st.read()
	must(t, err)
	if s.Messages[0].State == DeliveryStateSent {
		t.Fatal("delivery to a stale generation was recorded as sent")
	}
}

// The same fence is re-checked when the outcome is written back, so a restart that
// races the transport itself is never recorded as delivered to the new incarnation.
func TestDeliveryFencesMemberRestartDuringTransport(t *testing.T) {
	st := testStore(t)
	root := t.TempDir()
	marker := filepath.Join(root, "transport-started")
	bin := filepath.Join(root, "native")
	must(t, os.WriteFile(bin, []byte("#!/bin/sh\n: > \"$MARKER\"\nsleep 2\n"), 0700))
	cfg := config.Defaults()
	cfg.EngineCommands = map[config.Engine]config.Command{config.Codex: {Executable: bin}}
	var id string
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		m := s.Members["a"]
		m.Engine, m.EngineID, m.Cwd = config.Codex, "thread-7", root
		m.Env = map[string]string{"MARKER": marker}
		id = s.message("master", "a", "", "for the incarnation that was running", "").ID
		return nil
	}))
	done := make(chan error, 1)
	go func() { done <- st.deliver(id) }()
	// The marker proves transport is under way, so the claim fence already passed.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transport never started")
		}
	}
	must(t, st.update(func(s *State) error { s.Members["a"].Generation++; return nil }))
	must(t, <-done)
	s, err := st.read()
	must(t, err)
	if s.Messages[0].State == DeliveryStateSent {
		t.Fatal("transport that raced a restart was recorded as delivered to the new generation")
	}
}

// An unavailable recipient must leave a reason on the message. Returning with no
// reason made an undeliverable send indistinguishable from a successful one for
// every caller, not only the brief-report path.
func TestDeliveryRecordsUnavailableRecipient(t *testing.T) {
	for _, state := range []MemberState{MemberStateRemoved, MemberStateStopped, MemberStateStopping, MemberStateNeedsAttention, MemberStateCrashed} {
		t.Run(string(state), func(t *testing.T) {
			st := testStore(t)
			var id string
			if e := st.update(func(s *State) error {
				s.Members["a"].State = state
				id = s.message("master", "a", "", "text", "").ID
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			if e := st.deliver(id); e != nil {
				t.Fatalf("deliver: %v", e)
			}
			s, e := st.read()
			if e != nil {
				t.Fatal(e)
			}
			msg := s.Messages[0]
			if msg.State != DeliveryStatePending {
				t.Fatalf("state = %q, want pending so the runtime retries when the recipient returns", msg.State)
			}
			if !strings.Contains(msg.Error, string(state)) {
				t.Fatalf("error = %q, want it to name the recipient state %q", msg.Error, state)
			}
		})
	}
}

// The user has no inbox, so a reply would queue a message nothing can deliver and
// the runtime would retry it on every pass.
func TestReplyToUserOriginatedMessageRefused(t *testing.T) {
	st := testStore(t)
	if e := st.update(func(s *State) error {
		s.message(UserSender, "master", "", "brief-report request", "")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := messageCommand(st, "master", []string{"reply", "M1"}, options{"text": "here is the summary"}); e == nil {
		t.Fatal("reply to a user-originated message must be refused")
	}
	s, e := st.read()
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("messages = %d, want the refused reply not to be queued", len(s.Messages))
	}
	if s.Messages[0].State == DeliveryStateAcknowledged {
		t.Fatal("a refused reply must not acknowledge the request it failed to answer")
	}
	if e = st.syncMessages(); e != nil && strings.Contains(e.Error(), UserSender) {
		t.Fatalf("syncMessages reported an undeliverable user recipient: %v", e)
	}
}

// Reserving the sender identity keeps a recruited member from impersonating the
// human behind a brief-report request.
func TestMemberAddRefusesReservedUserID(t *testing.T) {
	st := testStore(t)
	if e := memberCommand(st, "master", []string{"add", UserSender}, options{"instructions": "impostor"}); e == nil {
		t.Fatal("member add must refuse the reserved user ID")
	}
	s, e := st.read()
	if e != nil {
		t.Fatal(e)
	}
	if s.Members[UserSender] != nil {
		t.Fatal("reserved user ID must never become a member")
	}
}

// Issue #24: the sender's generation may change while transport is under way.
// The outcome must still be recorded, or recovery re-sends a delivered message.
func TestDeliveryRecordsOutcomeAfterSenderRestart(t *testing.T) {
	st := testStore(t)
	root := t.TempDir()
	marker := filepath.Join(root, "transport-started")
	release := filepath.Join(root, "release")
	bin := filepath.Join(root, "native")
	must(t, os.WriteFile(bin, []byte("#!/bin/sh\n: > \"$MARKER\"\nwhile [ ! -e \"$RELEASE\" ]; do sleep 0.01; done\n"), 0700))
	cfg := config.Defaults()
	cfg.EngineCommands = map[config.Engine]config.Command{config.Codex: {Executable: bin}}
	var id string
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		m := s.Members["a"]
		m.Engine, m.EngineID, m.Cwd = config.Codex, "thread-7", root
		m.Env = map[string]string{"MARKER": marker, "RELEASE": release}
		id = s.message("b", "a", "", "sent by b", "").ID
		return nil
	}))
	sender := &Store{Dir: st.Dir, DB: st.DB, Actor: "b", Generation: 1}
	done := make(chan error, 1)
	go func() { done <- sender.deliver(id) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transport never started")
		}
	}
	must(t, st.update(func(s *State) error { s.Members["b"].Generation++; return nil }))
	must(t, os.WriteFile(release, nil, 0600))
	must(t, <-done)
	s, err := st.read()
	must(t, err)
	if got := s.Messages[0].State; got != DeliveryStateSent {
		t.Fatalf("state = %q, want sent: the recipient already has the message", got)
	}
}
