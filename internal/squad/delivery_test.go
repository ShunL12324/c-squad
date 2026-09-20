package squad

import (
	"strings"
	"testing"
)

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
	if e := memberCommand(st, "master", []string{"add", UserSender}, options{"role": "impostor"}); e == nil {
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
