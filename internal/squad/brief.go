package squad

// legacyBrief identifies historical outbox requests from the removed Brief
// report solely to prevent replay. Their records remain intact for audit.
func legacyBrief(m *Message) bool {
	return m.From == UserSender && m.To == "master" && m.RequestKey == UserSender+":brief:"+m.Task
}
