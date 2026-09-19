package squad

// agentPane keeps transport and liveness checks on the engine when a UI pane has
// focus. The window-zero fallback supports ledgers written before pane IDs existed.
func agentPane(m *Member) string {
	if m.Pane != "" {
		return m.Pane
	}
	return "=" + m.Session + ":"
}
