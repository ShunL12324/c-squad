package squad

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
)

// deliveryPaused lists the recipient states deliver waits out, keeping the
// message pending until the member is available again.
func deliveryPaused(state MemberState) bool {
	return state == MemberStateStopped || state == MemberStateStopping || state == MemberStateNeedsAttention || state == MemberStateCrashed
}

func (st *Store) deliver(id string) error {
	claimed := false
	var attempt string
	var recipientGen int
	if err := st.update(func(s *State) error {
		s.expireReports()
		for _, v := range s.Messages {
			if v.ID == id {
				if legacyBrief(v) {
					return nil
				}
				if v.State == DeliveryStateAcknowledged || v.State == DeliveryStateSuperseded {
					return nil
				}
				if v.State == DeliveryStateSent {
					return nil
				}
				if !s.Active {
					return ErrTeamStopped
				}
				member, e := s.member(v.To)
				if e != nil {
					return e
				}
				if member.State == MemberStateRemoved {
					// Messages queued before removal, or by internal notices,
					// would otherwise be retried on every runtime pass.
					v.abandonDelivery()
					return nil
				}
				if deliveryPaused(member.State) {
					// Returning without a reason left the sender no signal at all.
					// Record why delivery paused, exactly as the branch below does,
					// and keep the message pending so the runtime retries once the
					// recipient is available again.
					if v.State == DeliveryStatePending {
						v.Error = "recipient is " + string(member.State) + "; delivery paused until it is available"
					}
					return nil
				}
				if member.Engine == config.Claude && (member.EngineID == "" || member.Peer == "") {
					if v.State == DeliveryStatePending {
						v.Error = "recipient not registered yet; runtime will retry"
					}
					return nil
				}
				at, _ := time.Parse(time.RFC3339Nano, v.Attempt)
				if v.State == DeliveryStatePending && v.Attempt != "" {
					delay := time.Duration(1<<min(v.Attempts, 5)) * time.Second
					if time.Since(at) < delay {
						return nil
					}
				}
				if v.State == DeliveryStateSending {
					at, _ := time.Parse(time.RFC3339Nano, v.Attempt)
					if time.Since(at) < time.Minute {
						return nil
					}
				} else if v.State != DeliveryStatePending {
					return nil
				}
				v.State = DeliveryStateSending
				v.Attempt = now()
				v.Attempts++
				attempt = v.Attempt
				recipientGen = member.Generation
				claimed = true
				return nil
			}
		}
		return fmt.Errorf("unknown message: %w", ErrNotFound)
	}); err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	s, e := st.read()
	if e != nil {
		return e
	}
	var to string
	for _, v := range s.Messages {
		if v.ID == id {
			to = v.To
		}
	}
	unlock, e := filelock.Acquire(st.Dir, "member-"+to, false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e = st.read()
	if e != nil {
		return e
	}
	if s.Members[to] == nil || s.Members[to].Generation != recipientGen {
		return nil
	}
	var msg *Message
	for _, v := range s.Messages {
		if v.ID == id {
			msg = v
			break
		}
	}
	if msg == nil {
		return fmt.Errorf("unknown message: %w", ErrNotFound)
	}
	if msg.State != DeliveryStateSending || msg.Attempt != attempt || !s.reportCurrent(msg) {
		return nil
	}
	m, e := s.member(msg.To)
	if e != nil {
		return e
	}
	if m.State == MemberStateStopped || m.State == MemberStateRemoved || m.State == MemberStateStopping || m.State == MemberStateNeedsAttention || m.State == MemberStateCrashed {
		e = fmt.Errorf("recipient is %s", m.State)
		stateErr := st.update(func(s *State) error {
			for _, v := range s.Messages {
				if v.ID == id {
					v.State = DeliveryStatePending
					v.Error = e.Error()
				}
			}
			return nil
		})
		return errors.Join(e, stateErr)
	}
	// Refresh aggregated facts immediately before transport.
	msg.Text = s.reportText(msg)
	text := messageBody(msg, recipientGen, true)
	if m.Engine == config.Claude {
		if m.Peer == "" {
			e = fmt.Errorf("recipient inbox not registered yet")
		} else {
			var c net.Conn
			c, e = net.DialTimeout("unix", m.Peer, 2*time.Second)
			if e == nil {
				e = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
				frame, _ := json.Marshal(claudePeerFrame(s, msg, recipientGen))
				if e == nil {
					_, e = c.Write(append(frame, '\n'))
				}
				_ = c.Close()
			}
		}
	} else {
		if m.EngineID == "" {
			e = st.startCodexInput(s, m, text)
		} else {
			_, e = s.engineHelper(m, "queue", "--thread", m.EngineID, "--message", text)
		}
	}
	deliveryErr := e
	// The outcome is a fact about the transport, not a write on the sender's
	// behalf: a sender restarted meanwhile must not leave a delivered message in
	// sending, where recovery would queue it again. The attempt and recipient
	// generation below still fence every stale write.
	recorder := &Store{Dir: st.Dir, DB: st.DB}
	err := recorder.update(func(s *State) error {
		for _, v := range s.Messages {
			if v.ID == id && v.State == DeliveryStateSending && v.Attempt == attempt && s.Members[v.To] != nil && s.Members[v.To].Generation == recipientGen {
				if deliveryErr != nil {
					v.State = DeliveryStatePending
					v.Error = deliveryErr.Error()
				} else {
					v.State = DeliveryStateSent
					v.Text = msg.Text
					v.Error = ""
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return deliveryErr
}
func (st *Store) syncMessages() error {
	s, e := st.read()
	if e != nil {
		return e
	}
	// Delivery writes the ledger; on a pinned team only its own build does it.
	if !isPinnedBuild(s) {
		return nil
	}
	var failed []string
	for _, m := range s.Messages {
		// Sent records remain visible without ACK; only unfinished transport needs work.
		if !legacyBrief(m) && (m.State == DeliveryStatePending || m.State == DeliveryStateSending) {
			if e = st.deliver(m.ID); e != nil {
				failed = append(failed, m.ID+": "+e.Error())
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("persisted, pending delivery: %s", strings.Join(failed, "; "))
	}
	return nil
}
