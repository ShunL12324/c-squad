// Package teamui renders read-only team panels without owning agent terminals.
package teamui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Member is a presentation snapshot of an agent and its current assignments.
type Member struct{ ID, Engine, State, Color, Tasks string }

// Task is a presentation snapshot of a task and its delivery evidence.
type Task struct{ ID, Title, State, Owner, Color, Detail string }

// Snapshot contains read-only data from the authoritative team ledger.
type Snapshot struct {
	Team               string
	Members            []Member
	Tasks              []Task
	Requests, Activity []string
	Active             bool
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
	kind, current, tab                   string
	data                                 Snapshot
	load                                 Source
	act                                  Handler
	width, height, selected, top, offset int
	selectedID                           string
	err                                  error
}

// Run owns only its pane's terminal; the native agent continues in another pane.
func Run(kind, current string, load Source, act Handler) error {
	m := model{kind: kind, current: current, tab: "tasks", load: load, act: act, width: 24, height: 24}
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
	return len(m.data.Tasks)
}
func (m model) rows() int {
	if m.kind == "members" {
		return max(1, (m.height-4)/3)
	}
	return max(1, min(5, (m.height-7)/4))
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
		m.selectedID = m.data.Tasks[m.selected].ID
	}
}
func (m *model) reveal() {
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+m.rows() {
		m.top = m.selected - m.rows() + 1
	}
	m.top = max(0, m.top)
}
func (m model) open() tea.Cmd {
	if m.count() == 0 {
		return nil
	}
	id := ""
	if m.kind == "members" {
		id = m.data.Members[m.selected].ID
	} else {
		id = m.data.Tasks[m.selected].Owner
	}
	if id == "" {
		return nil
	}
	return m.action(Action{Kind: "open", Member: id})
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
		if v.err == nil {
			m.data = v.data
			if !v.data.Active {
				return m, tea.Quit
			}
			for i := 0; i < m.count(); i++ {
				id := ""
				if m.kind == "members" {
					id = m.data.Members[i].ID
				} else {
					id = m.data.Tasks[i].ID
				}
				if id == m.selectedID {
					m.selected = i
				}
			}
			m.selected = max(0, min(m.selected, m.count()-1))
			m.remember()
			m.reveal()
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
		switch v.String() {
		case "q", "esc", "ctrl+c":
			return m, m.action(Action{Kind: "close"})
		case "m":
			return m, m.action(Action{Kind: "open", Member: "master"})
		case "enter":
			return m, m.open()
		case "down", "j":
			m.move(1)
		case "up", "k":
			m.move(-1)
		case "pgdown":
			m.offset += max(1, m.height/2)
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height/2))
		case "tab":
			switch m.tab {
			case "tasks":
				m.tab = "activity"
			case "activity":
				m.tab = "requests"
			default:
				m.tab = "tasks"
			}
			m.offset = 0
		}
	case tea.MouseMsg:
		if v.Action != tea.MouseActionPress {
			return m, nil
		}
		switch v.Button {
		case tea.MouseButtonLeft:
			if m.kind == "members" {
				i := m.top + (v.Y-2)/3
				if v.Y >= 2 && v.Y < 2+m.rows()*3 && i < m.count() {
					m.selected = i
					m.remember()
					return m, m.open()
				}
			}
			if m.kind == "tasks" {
				if v.Y == 0 {
					tabs := []string{"tasks", "activity", "requests"}
					m.tab = tabs[min(2, v.X/max(1, m.width/3))]
					m.offset = 0
				} else if m.tab == "tasks" && v.Y >= 2 && v.Y < 2+m.rows()*2 {
					i := m.top + (v.Y-2)/2
					if i < m.count() {
						m.selected = i
						m.remember()
						m.offset = 0
					}
				}
			}
		case tea.MouseButtonWheelDown:
			if m.kind == "members" || m.tab == "tasks" && v.Y < 2+m.rows()*2 {
				m.move(1)
			} else {
				m.offset += 3
			}
		case tea.MouseButtonWheelUp:
			if m.kind == "members" || m.tab == "tasks" && v.Y < 2+m.rows()*2 {
				m.move(-1)
			} else {
				m.offset = max(0, m.offset-3)
			}
		}
	}
	return m, nil
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
	wrapped := lipgloss.NewStyle().Width(max(1, m.width-2)).Render(clean(text))
	lines := strings.Split(wrapped, "\n")
	offset := min(m.offset, max(0, len(lines)-available))
	return lines[offset:min(len(lines), offset+available)]
}

// View renders a bounded panel; the parent tmux pane owns its dimensions.
func (m model) View() string {
	var lines []string
	if m.kind == "members" {
		lines = []string{paint(" C SQUAD", "121", true), line(" "+m.data.Team, m.width)}
		for i := m.top; i < min(m.count(), m.top+m.rows()); i++ {
			member := m.data.Members[i]
			mark := "● "
			if member.ID == m.current {
				mark = "◆ "
			}
			lines = append(lines, paint(line(" "+mark+member.ID, m.width), member.Color, i == m.selected), line("   "+member.Engine+" · "+member.State, m.width), line("   "+member.Tasks, m.width))
		}
	} else {
		w := max(1, m.width/3)
		header := ""
		for _, tab := range []string{"tasks", "activity", "requests"} {
			label := map[string]string{"tasks": "Tasks", "activity": "Activity", "requests": "Requests"}[tab]
			if tab == "requests" {
				label = fmt.Sprintf("Asks %d", len(m.data.Requests))
			}
			header += paint(fmt.Sprintf("%-*s", w, line(label, w)), "121", m.tab == tab)
		}
		lines = []string{header, fmt.Sprintf("%d tasks · %d open requests", len(m.data.Tasks), len(m.data.Requests))}
		if m.tab == "tasks" {
			for i := m.top; i < min(m.count(), m.top+m.rows()); i++ {
				t := m.data.Tasks[i]
				lines = append(lines, paint(line(" "+t.ID+" "+t.Title, m.width), t.Color, i == m.selected), line("   "+t.State+" · "+t.Owner, m.width))
			}
			if m.count() == 0 {
				lines = append(lines, " No tasks yet.", " Ask Master to plan work.")
			} else {
				lines = append(lines, strings.Repeat("─", m.width))
				lines = append(lines, m.details(m.data.Tasks[m.selected].Detail, m.height-len(lines)-2)...)
			}
		} else {
			text := "No activity yet."
			if m.tab == "activity" && len(m.data.Activity) > 0 {
				text = strings.Join(m.data.Activity, "\n\n")
			}
			if m.tab == "requests" {
				text = "No unanswered requests."
				if len(m.data.Requests) > 0 {
					text = strings.Join(m.data.Requests, "\n\n")
				}
			}
			lines = append(lines, m.details(text, m.height-4)...)
		}
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), max(0, m.height-2))]
	footer := "Click / ↑↓ · Enter open"
	if m.kind == "tasks" {
		footer = "Tab views · PgUp/PgDn"
	}
	lines = append(lines, line(footer, m.width))
	last := "q close · m Master"
	if m.err != nil {
		last = m.err.Error()
	}
	lines = append(lines, line(last, m.width))
	for i, s := range lines {
		lines[i] = ansi.Truncate(s, m.width, "")
	}
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}
