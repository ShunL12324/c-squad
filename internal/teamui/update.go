package teamui

import tea "github.com/charmbracelet/bubbletea"

func (m model) updateSnapshot(v snapshotMsg) (tea.Model, tea.Cmd) {
	m.err = v.err
	// The first read decides where the list starts; later reads must not
	// fight a user who has scrolled the panel with the wheel.
	first := m.loading
	m.loading = false
	if v.err == nil {
		m.data = v.data
		if !v.data.Active {
			return m, tea.Quit
		}
		found := false
		for i := 0; i < m.count(); i++ {
			id := ""
			if m.kind == "members" {
				id = m.data.Members[i].ID
			} else {
				id = m.tasks()[i].ID
			}
			if id == m.selectedID {
				found = true
				m.selected = i
			}
		}
		if !found {
			m.detail = false
		}
		m.selected = max(0, min(m.selected, m.count()-1))
		if m.count() == 0 {
			m.detail = false
		}
		m.remember()
		if !found || first {
			m.reveal()
		}
		// Content that shrank or rewrapped must not leave presses to undo.
		m.offset = min(m.offset, m.maxOffset())
	}
	return m, tick()
}

func (m model) updateKey(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.kind == "header" {
		return m, nil
	}
	switch v.String() {
	case "q", "esc", "ctrl+c":
		if m.detail && v.String() != "ctrl+c" {
			m.detail = false
			m.offset = 0
			return m, nil
		}
		if m.kind == "members" {
			return m, nil
		}
		return m, m.action(Action{Kind: "close"})
	case "enter":
		if m.kind == "members" && m.count() > 0 {
			return m.navigate(m.data.Members[m.selected].ID)
		}
		if m.kind == "tasks" && m.count() > 0 {
			m.detail = true
			m.offset = 0
		}
		return m, nil
	case "left", "right":
		if m.kind == "tasks" && !m.detail {
			m.filterTasks(v.String() == "right")
		}
	case "o":
		if m.kind == "tasks" {
			return m, m.open()
		}
	case "b", "r":
		// One key for both: a retry is the same request, and the ledger
		// reuses the message rather than queueing a second one.
		if m.kind == "tasks" {
			return m.brief()
		}
	case "g":
		// Master is offered, never opened for the user: asking about one
		// card should not move someone who is working through several.
		if m.kind == "tasks" {
			return m, m.action(Action{Kind: "open", Member: "master"})
		}
	case "down", "j":
		if m.kind == "tasks" && m.detail {
			m.offset = min(m.offset+1, m.maxOffset())
		} else {
			m.move(1)
		}
	case "up", "k":
		if m.kind == "tasks" && m.detail {
			m.offset = max(0, min(m.offset, m.maxOffset())-1)
		} else {
			m.move(-1)
		}
	case "pgdown":
		m.offset = max(0, min(m.offset+max(1, m.height/2), m.maxOffset()))
	case "pgup":
		m.offset = max(0, min(m.offset, m.maxOffset())-max(1, m.height/2))

	}
	return m, nil
}

func (m model) updateMouse(v tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.kind == "header" {
		return m, nil
	}
	if v.Action != tea.MouseActionPress {
		return m, nil
	}
	switch v.Button {
	case tea.MouseButtonLeft:
		if v.Y >= m.height-2 {
			if v.Y == m.height-1 {
				for _, button := range m.footerButtons() {
					if v.X >= button.start && v.X < button.end && v.X < m.width {
						switch button.action {
						case "back":
							m.detail, m.offset = false, 0
							return m, nil
						}
					}
				}
			}
			return m, nil
		}
		if m.kind == "members" {
			_, hits := m.memberCards()
			for _, hit := range hits {
				if v.Y >= hit.start && v.Y < hit.end {
					return m.navigate(m.data.Members[hit.index].ID)
				}
			}
		}
		if m.kind == "tasks" {
			if v.Y == taskTitleRow && v.X >= m.width-5 && v.X < m.width-2 {
				return m, m.action(Action{Kind: "close"})
			}
			if m.detail {
				if v.Y == taskFilterRow {
					_, buttons := detailRow(m.width)
					for _, button := range buttons {
						if button.action == "brief" && v.X >= button.start && v.X < button.end {
							return m.brief()
						}
					}
					// Every other cell on this row still goes back, as it
					// did before the row carried a second button.
					m.detail = false
					m.offset = 0
				}
				return m, nil
			}
			if v.Y == taskFilterRow {
				// Gutters fall to the segment they sit beside, so the whole
				// row stays clickable with no dead columns.
				left, _ := m.filterSplit()
				m.filterTasks(v.X >= 2+left)
			} else {
				_, hits := m.taskCards()
				for _, hit := range hits {
					if v.Y >= hit.start+taskHeaderRows && v.Y < hit.end+taskHeaderRows {
						m.selected = hit.index
						m.remember()
						for _, button := range hit.buttons {
							if v.Y != button.row+taskHeaderRows || v.X < button.start || v.X >= button.end {
								continue
							}
							if button.action == "brief" {
								return m.brief()
							}
							m.detail, m.offset = true, 0
							break
						}
						break
					}
				}
			}
		}
	case tea.MouseButtonWheelDown:
		m.scroll(1)
	case tea.MouseButtonWheelUp:
		m.scroll(-1)
	}
	return m, nil
}
