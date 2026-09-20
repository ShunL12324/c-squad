package squad

import (
	"errors"
	"fmt"
)

func messageCommand(st *Store, actor string, p []string, o options) error {
	var ids []string
	e := st.update(func(s *State) error {
		if p[0] == "reply" {
			if len(p) < 2 {
				return errors.New("message ID required")
			}
			for _, m := range s.Messages {
				if m.ID == p[1] {
					if m.To != actor {
						return errors.New("not your message")
					}
					// The user has no inbox. A reply would queue a message addressed
					// to a member that does not exist, which deliver cannot resolve
					// and the runtime would retry forever, logging a cycle error on
					// every pass.
					if m.From == UserSender {
						return errors.New("this request came from the user, who has no CLI inbox; answer them in your own session")
					}
					if o["text"] == "" {
						return errors.New("--text required")
					}
					for _, old := range s.Messages {
						if old.From == actor && old.ReplyTo == m.ID {
							ids = append(ids, old.ID)
							return nil
						}
					}
					m.State = DeliveryStateAcknowledged
					ids = append(ids, s.message(actor, m.From, m.Task, o["text"], m.ID).ID)
					return nil
				}
			}
			return fmt.Errorf("unknown message: %w", ErrNotFound)
		}
		if len(p) < 2 {
			return errors.New("message subcommand required")
		}
		switch p[1] {
		case "inbox":
			a := []*Message{}
			for _, m := range s.Messages {
				if m.To == actor && m.State != DeliveryStateAcknowledged {
					a = append(a, m)
				}
			}
			return jsonOut(a)
		case "retry":
			if actor != "master" || len(p) < 3 {
				return errors.New("master must specify message ID")
			}
			for _, m := range s.Messages {
				if m.ID == p[2] {
					if m.State == DeliveryStateAcknowledged {
						return errors.New("message already acknowledged")
					}
					m.State = DeliveryStatePending
					m.Attempt = ""
					m.Attempts = 0
					m.Error = ""
					ids = append(ids, m.ID)
					return nil
				}
			}
			return fmt.Errorf("message not found: %w", ErrNotFound)
		case "ack":
			if len(p) < 3 {
				return errors.New("message ID required")
			}
			for _, m := range s.Messages {
				if m.ID == p[2] && m.To == actor {
					m.State = DeliveryStateAcknowledged
					return nil
				}
			}
			return fmt.Errorf("message not found for recipient: %w", ErrNotFound)
		case "send", "broadcast":
			if o["text"] == "" {
				return errors.New("--text required")
			}
			recipients := []string{}
			if p[1] == "send" {
				if len(p) < 3 {
					return errors.New("recipient required")
				}
				recipients = append(recipients, p[2])
			} else {
				if o["task"] == "" && o["all"] != "true" {
					return errors.New("broadcast requires --task ID or explicit --all")
				}
				for id, m := range s.Members {
					if id != actor && m.State != MemberStateRemoved && m.State != MemberStateStopped {
						if o["task"] != "" {
							t, e := s.task(o["task"])
							if e != nil {
								return e
							}
							if !contains(t.Participants, id) {
								continue
							}
						}
						recipients = append(recipients, id)
					}
				}
			}
			for _, id := range recipients {
				if _, e := s.member(id); e != nil {
					return e
				}
				key := ""
				if o["request-id"] != "" {
					key = actor + ":" + o["request-id"] + ":" + id
				}
				existing := ""
				if key != "" {
					for _, old := range s.Messages {
						if old.RequestKey == key {
							existing = old.ID
							break
						}
					}
				}
				if existing != "" {
					ids = append(ids, existing)
					continue
				}
				msg := s.message(actor, id, o["task"], o["text"], "")
				msg.RequestKey = key
				ids = append(ids, msg.ID)
			}
			return nil
		}
		return errors.New("unknown message operation")
	})
	if e != nil {
		return e
	}
	for _, id := range ids {
		// The durable outbox retains failed deliveries for runtime retry.
		_ = st.deliver(id)
	}
	if len(ids) > 0 {
		return jsonOut(map[string]any{"messages": ids, "note": "persisted; inspect board for transport/ack status"})
	}
	return nil
}
