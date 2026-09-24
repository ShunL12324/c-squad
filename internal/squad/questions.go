package squad

import (
	"errors"
	"fmt"
	"slices"
)

func helpCommand(st *Store, actor string, p []string, o options) error {
	if len(p) == 0 {
		return errors.New("help subcommand required (use --help for usage)")
	}
	var message string
	var result any
	e := st.update(func(s *State) error {
		switch p[0] {
		case "list":
			result = s.Questions
		case "request":
			if o["text"] == "" {
				return errors.New("--text required")
			}
			if o["task"] != "" {
				t, e := s.task(o["task"])
				if e != nil {
					return e
				}
				if t.State.terminal() || t.State == TaskPhaseMerging {
					return errors.New("cannot block a completed, cancelled or merging task")
				}
				if actor != "master" && !slices.Contains(t.Participants, actor) {
					return errors.New("not a task participant")
				}
			}
			q := &Question{ID: s.next("Q"), Member: actor, Task: o["task"], Text: o["text"], State: QuestionStateOpen}
			s.Questions[q.ID] = q
			s.Members[actor].State = MemberStateWaitingMaster
			msg := s.message(actor, "master", q.Task, "Help request "+q.ID+": "+q.Text, "")
			msg.Report = &ReportReference{Kind: "decision", Question: q.ID}
			message = msg.ID
			result = q
		case "answer":
			if actor != "master" {
				return fmt.Errorf("only master answers escalations: %w", ErrMasterRequired)
			}
			if len(p) < 2 || o["text"] == "" {
				return errors.New("question ID and --text required")
			}
			q := s.Questions[p[1]]
			if q == nil {
				return fmt.Errorf("unknown question: %w", ErrNotFound)
			}
			if q.State == QuestionStateAnswered {
				return errors.New("already answered")
			}
			q.Answer = o["text"]
			q.State = QuestionStateAnswered
			s.expireReports()
			m := s.Members[q.Member]
			if m != nil && m.State == MemberStateWaitingMaster {
				m.State = MemberStateIdle
				for _, other := range s.Questions {
					if other.Member == q.Member && other.State == QuestionStateOpen {
						m.State = MemberStateWaitingMaster
					}
				}
			}
			// The answer stays on the question record; a removed asker has no
			// inbox, so queueing a message would only be retried forever.
			if _, e := s.recipient(q.Member); e == nil {
				message = s.message(actor, q.Member, q.Task, "Answer to "+q.ID+": "+q.Answer, "").ID
			}
			result = q
		default:
			return errors.New("unknown help operation")
		}
		return nil
	})
	if e != nil {
		return e
	}
	if message != "" {
		// The durable outbox retains failed deliveries for runtime retry.
		_ = st.deliver(message)
	}
	return queryOut(o, result)
}
