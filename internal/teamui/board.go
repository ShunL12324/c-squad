package teamui

func (m Milestone) complete() bool {
	return m.State == "approved" || m.State == "reported" && !m.Gate
}

// Done and cancelled work share the history tab; other states stay active.
func finished(state string) bool { return state == "done" || state == "cancelled" }
