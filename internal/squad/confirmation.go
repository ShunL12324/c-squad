package squad

// UserConfirmation is a record from ledgers written while the task panel asked
// the user to confirm completion. Completion is now derived from the agent
// workflow; the field stays so those ledgers load and keep their audit trail.
type UserConfirmation struct {
	At         string    `json:"at"`
	Actor      string    `json:"actor"`
	Submission string    `json:"submission,omitempty"`
	Candidate  string    `json:"candidate,omitempty"`
	Phase      TaskPhase `json:"phase"`
}
