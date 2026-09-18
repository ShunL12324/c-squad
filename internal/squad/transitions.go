package squad

// resetRuntime clears only transient identities after the previous process tree
// has stopped. Generation fencing and native conversation identity are separate.
func (m *Member) resetRuntime() {
	m.State = MemberStateStarting
	m.Peer = ""
	m.Pane = ""
	m.RunnerPID = 0
	m.EnginePID = 0
	m.ProcessStart = ""
	m.Processes = nil
	m.ObservedAt = ""
}

func (m *Message) resetDelivery() {
	m.State = DeliveryStatePending
	m.Attempt = ""
	m.Attempts = 0
	m.Error = ""
	m.RecipientGeneration = 0
}
