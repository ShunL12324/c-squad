// Package teamui renders read-only team panels without owning agent terminals.
package teamui

import (
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Member is a presentation snapshot of an agent and its current assignments.
type Member struct{ ID, Engine, State, Color, Tasks, Cwd string }

// Task is a presentation snapshot of a task and its delivery evidence.
type Task struct {
	ID, Title, State, Owner, Color, Progress, Detail string
	Milestones                                       []Milestone
}

// Milestone retains reporting and approval states without inferring completion.
type Milestone struct {
	Name, State string
	Gate        bool
}

// Snapshot contains read-only data from the authoritative team ledger.
type Snapshot struct {
	Team    string
	Members []Member
	Tasks   []Task
	Switch  string
	Active  bool
}

// Action describes a UI request; the controller owns session and pane operations.
type Action struct{ Kind, Member string }

// Source reads snapshots without refreshing or mutating agent state.
type Source func() (Snapshot, error)

// Handler performs a navigation action outside the renderer.
type Handler func(Action) error

type snapshotMsg struct {
	data Snapshot
	err  error
}
type actionMsg struct {
	err  error
	quit bool
}
type tickMsg time.Time

type model struct {
	kind, current                        string
	data                                 Snapshot
	load                                 Source
	act                                  Handler
	width, height, selected, top, offset int
	selectedID                           string
	completed                            bool
	detail                               bool
	loading                              bool
	err                                  error
}

// Run owns only its pane's terminal; the native agent continues in another pane.
func Run(kind, current string, load Source, act Handler) error {
	// These panels run inside tmux, where color identifies members and states.
	// Agent launchers may export NO_COLOR for their own captured CLI output;
	// do not let that inherited setting disable the interactive panel palette.
	if lipgloss.ColorProfile() != termenv.TrueColor {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}

	m := model{kind: kind, current: current, load: load, act: act, width: 24, height: 24, loading: true}
	if kind == "members" {
		m.selectedID = current
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
func (m model) read() tea.Msg { d, e := m.load(); return snapshotMsg{d, e} }
func tick() tea.Cmd           { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }

// Init schedules the first ledger read.
func (m model) Init() tea.Cmd { return m.read }
func (m model) action(a Action) tea.Cmd {
	return func() tea.Msg { return actionMsg{m.act(a), a.Kind == "close"} }
}
func (m model) count() int {
	if m.kind == "members" {
		return len(m.data.Members)
	}
	return len(m.tasks())
}
func (m model) rows() int {
	if m.kind == "members" {
		reserved := memberHeaderRows + 2
		if m.hasMaster() {
			reserved += memberBlockRows + 2
		}
		return max(1, (m.height-reserved)/memberBlockRows)
	}
	return max(1, (m.height-4)/8)
}
func (m *model) move(delta int) {
	m.selected = max(0, min(m.count()-1, m.selected+delta))
	m.offset = 0
	m.remember()
	m.reveal()
}
func (m *model) remember() {
	if m.count() == 0 {
		m.selectedID = ""
		return
	}
	if m.kind == "members" {
		m.selectedID = m.data.Members[m.selected].ID
	} else {
		m.selectedID = m.tasks()[m.selected].ID
	}
}
func (m *model) reveal() {
	if m.kind == "members" && m.hasMaster() {
		m.top = max(1, m.top)
		if m.selected == 0 {
			return
		}
	}
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+m.rows() {
		m.top = m.selected - m.rows() + 1
	}
	m.top = max(0, m.top)
	if m.kind == "tasks" {
		for m.top < m.selected {
			_, hits := m.taskCards()
			if len(hits) > 0 && hits[len(hits)-1].index >= m.selected {
				break
			}
			m.top++
		}
	}
}
func (m model) open() tea.Cmd {
	if m.count() == 0 {
		return nil
	}
	id := ""
	if m.kind == "members" {
		id = m.data.Members[m.selected].ID
	} else {
		id = m.tasks()[m.selected].Owner
	}
	if id == "" {
		return nil
	}
	return m.action(Action{Kind: "open", Member: id})
}

// navigate restores this session's cursor before leaving. Each tmux session
// owns a persistent sidebar; an outgoing destination must not become its saved
// selection when the user returns to this session later.
func (m model) navigate(id string) (tea.Model, tea.Cmd) {
	if m.kind == "members" {
		for i, member := range m.data.Members {
			if member.ID == m.current {
				m.selected = i
				m.remember()
				m.reveal()
				break
			}
		}
	}
	return m, m.action(Action{Kind: "open", Member: id})
}

// Update handles input and asynchronous reads without blocking terminal rendering.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.reveal()
	case snapshotMsg:
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
		}
		return m, tick()
	case tickMsg:
		return m, m.read
	case actionMsg:
		m.err = v.err
		if v.quit && v.err == nil {
			return m, tea.Quit
		}
	case tea.KeyMsg:
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
		case "down", "j":
			if m.kind == "tasks" && m.detail {
				m.offset++
			} else {
				m.move(1)
			}
		case "up", "k":
			if m.kind == "tasks" && m.detail {
				m.offset = max(0, m.offset-1)
			} else {
				m.move(-1)
			}
		case "pgdown":
			m.offset += max(1, m.height/2)
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height/2))

		}
	case tea.MouseMsg:
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
							m.detail = v.Y == hit.button+taskHeaderRows && v.X >= 2 && v.X < 18
							m.remember()
							if m.detail {
								m.offset = 0
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
	}
	return m, nil
}

// scroll moves the viewport independently of keyboard selection.
func (m *model) scroll(direction int) {
	if m.kind == "members" {
		first := 0
		if m.hasMaster() {
			first = 1
		}
		m.top = max(first, min(max(first, m.count()-m.rows()), max(first, m.top)+direction))
		return
	}
	m.offset = max(0, m.offset+3*direction)
	if !m.detail {
		total := 0
		for i := m.top; i < len(m.tasks()); i++ {
			total += len(m.taskCard(i))
		}
		m.offset = min(m.offset, max(0, total-max(0, m.height-taskHeaderRows-2)))
	}
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
func line(s string, w int) string {
	return ansi.Truncate(strings.ReplaceAll(clean(s), "\n", " "), max(1, w), "…")
}
func paint(s, color string, selected bool) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if selected {
		style = style.Bold(true).Background(lipgloss.Color("236"))
	}
	return style.Render(s)
}
func (m model) details(text string, available int) []string {
	if available < 1 {
		return nil
	}
	wrapped := lipgloss.NewStyle().Width(max(1, m.width-6)).Render(clean(text))
	lines := strings.Split(wrapped, "\n")
	offset := min(m.offset, max(0, len(lines)-available))
	lines = lines[offset:min(len(lines), offset+available)]
	for i := range lines {
		lines[i] = "   " + lines[i]
	}
	return lines
}

// View renders a bounded panel; the parent tmux pane owns its dimensions.
func (m model) View() string {
	if m.kind == "header" {
		return m.workspaceHeader()
	}
	var lines []string
	switch {
	case m.loading:
		// The pane is laid out before the first ledger read returns. Fill the
		// fixed region with a placeholder rather than leaving it blank.
		lines = []string{"", textStyle("  Loading…", muted, false)}
	case m.kind == "members":
		lines = m.memberBlocks()
	default:
		lines = m.boardView()
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), max(0, m.height-2))]
	lines = append(lines, m.footer()...)
	base := lipgloss.NewStyle().Background(lipgloss.Color(canvas)).Foreground(lipgloss.Color(foreground))
	for i, s := range lines {
		s = ansi.Truncate(s, m.width, "")
		// Paint trailing cells explicitly; erased or unstyled cells otherwise
		// inherit the surrounding terminal's background during redraws.
		lines[i] = base.Render(s + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(s))))
	}
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}
