package squad

import (
	"errors"
	"fmt"

	"github.com/ShunL12324/c-squad/internal/teamui"
)

// briefKey identifies the one live brief-report request for a task. At most one
// unacknowledged request carries it, so a second press reuses the message that
// is already in flight instead of queueing another, and a retry stays a retry.
func briefKey(id string) string { return UserSender + ":brief:" + id }

// briefText is generated from the ledger, never typed by anyone. The closing
// lines matter as much as the questions: an agent reading a status request must
// not read it as licence to act on the task.
func briefText(s *State, t *Task) string {
	owner := t.Owner
	if owner == "" {
		owner = "unassigned"
	}
	return fmt.Sprintf(`[brief-report request from the user]
Team %s, task %s %q (state: %s, owner: %s).
The user is asking YOU (master) to summarise this task for them, in their language, in your own session:
1) the goal; 2) what is done; 3) what remains; 4) current blockers;
5) if finished: the result, and exactly how it was verified - evidence kinds and SHA, merge commit, or the explicit absence of them.
Read the ledger first (task inspect %s); ask the members if you need to.
This request changes no task state. Do NOT approve, reopen, merge, reassign, restart or re-run anything because of it.
Answer the user directly; do not reply to this message through the CLI.`,
		s.ID, t.ID, t.Title, t.State, owner, t.ID)
}

// briefCommand is the CLI entry point. The panel calls briefRequest directly:
// it owns a terminal that a JSON dump would corrupt.
func briefCommand(st *Store, actor, id string) error {
	note, e := briefRequest(st, actor, id)
	if e != nil {
		return e
	}
	return jsonOut(map[string]string{"task": id, "note": note})
}

// briefRequest asks master to summarise one task for the user, and reports what
// became of the request. It is deliberately not part of the task operation
// switch: it reads the task and writes only a message, so it must work for a
// done or merging task that the switch rejects, and it must leave the task
// itself untouched.
func briefRequest(st *Store, actor, id string) (string, error) {
	if actor != "master" {
		return "", fmt.Errorf("only master may request a brief report: %w", ErrMasterRequired)
	}
	s, e := st.read()
	if e != nil {
		return "", e
	}
	if !s.Active {
		return "", ErrTeamStopped
	}
	if _, e = s.task(id); e != nil {
		return "", e
	}
	// Check availability before queueing, so an absent master is reported to the
	// user instead of leaving a request nothing will ever collect.
	if masterGone(s) {
		return "", errors.New("no master session to ask; recover the team first with csquad recover")
	}
	msgID, e := briefEnqueue(st, id)
	if e != nil {
		return "", e
	}
	// The durable outbox keeps the request whatever happens here, so a transport
	// failure is reported to the user rather than raised: the runtime retries it,
	// and pressing again reuses this same message.
	deliveryErr := st.deliver(msgID)
	s, e = st.read()
	if e != nil {
		return "", e
	}
	for _, msg := range s.Messages {
		if msg.ID != msgID {
			continue
		}
		return briefNote(msg, deliveryErr), nil
	}
	return "", fmt.Errorf("brief request %s: %w", msgID, ErrNotFound)
}

// briefEnqueue records the request and returns the message carrying it. The key
// admits one live request per task, so a second press finds the message already
// in flight and retries that one rather than queueing a duplicate. The task is
// read and never written: asking about work is not an operation on it.
func briefEnqueue(st *Store, id string) (string, error) {
	var msgID string
	e := st.update(func(s *State) error {
		t, e := s.task(id)
		if e != nil {
			return e
		}
		key := briefKey(id)
		for _, old := range s.Messages {
			if old.RequestKey != key {
				continue
			}
			if old.State != DeliveryStateAcknowledged {
				old.resetDelivery()
				msgID = old.ID
				return nil
			}
			// Master answered the last one, so the key is spent. Release it
			// before reusing it, or the scan above would keep finding the
			// answered message forever and the user could never ask again.
			old.RequestKey = ""
		}
		msg := s.message(UserSender, "master", t.ID, briefText(s, t), "")
		msg.RequestKey = key
		msgID = msg.ID
		s.event(UserSender, "brief_requested", t.ID)
		return nil
	})
	return msgID, e
}

// briefNote is the one line the panel shows the user about their request. It
// never claims more than the outbox actually did.
func briefNote(msg *Message, deliveryErr error) string {
	reason := msg.Error
	if deliveryErr != nil {
		reason = deliveryErr.Error()
	}
	switch {
	case msg.State == DeliveryStateSent && reason == "":
		return "Asked master · " + msg.ID
	case msg.State == DeliveryStateNeedsAttention:
		return "Master has not picked it up · " + msg.ID
	case reason != "":
		return "Queued · " + msg.ID + " · " + reason
	default:
		return "Queued for master · " + msg.ID
	}
}

// briefFor projects the live request for a task so a respawned panel still shows
// what the user asked for. An acknowledged request is finished and shows nothing.
func briefFor(s *State, id string) teamui.Brief {
	key := briefKey(id)
	for _, msg := range s.Messages {
		if msg.RequestKey == key && msg.State != DeliveryStateAcknowledged {
			return teamui.Brief{MessageID: msg.ID, State: string(msg.State), Error: msg.Error}
		}
	}
	return teamui.Brief{}
}
