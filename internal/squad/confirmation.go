package squad

import "errors"

// UserConfirmation is independent of technical approval, evidence and Git state.
// It snapshots the delivery being accepted and never advances the workflow.
type UserConfirmation struct {
	At         string    `json:"at"`
	Actor      string    `json:"actor"`
	Submission string    `json:"submission,omitempty"`
	Candidate  string    `json:"candidate,omitempty"`
	Phase      TaskPhase `json:"phase"`
}

func (st *Store) confirmTask(id string) (string, error) {
	err := st.update(func(s *State) error {
		t, err := s.task(id)
		if err != nil {
			return err
		}
		if t.State != TaskPhaseDone {
			return errors.New("technical delivery must finish before user confirmation; inspect the task or ask master for a brief")
		}
		if t.UserConfirmation != nil {
			return nil
		}
		t.UserConfirmation = &UserConfirmation{At: now(), Actor: UserSender, Submission: t.Submission, Candidate: t.Candidate, Phase: t.State}
		s.event(UserSender, "task_confirmed", id)
		return nil
	})
	return "User confirmed delivery", err
}
