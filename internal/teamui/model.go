// Package teamui renders read-only team panels without owning agent terminals.
package teamui

import (
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Member is a presentation snapshot of an agent and its current assignments.
// Branch and Commit describe the member's own working directory.
type Member struct {
	ID, Engine, State, Color, Tasks, Cwd string
	Branch, Commit                       string
	Worktree                             bool
}

// Task is a presentation snapshot of a task and its delivery evidence.
type Task struct {
	ID, Title, State, Owner, Color, Progress, Detail, Note string
	Completion                                             string
	Milestones                                             []Milestone
}

// Milestone describes one task checkpoint and whether it gates completion.
type Milestone struct {
	Name, State string
	Gate        bool
}

// Snapshot is the team state displayed by a panel at one point in time.
type Snapshot struct {
	Team    string
	Members []Member
	Tasks   []Task
	Switch  string
	Active  bool
}

// Action requests a panel operation for a member or task.
type Action struct{ Kind, Member, Task string }

// Source loads the latest team snapshot for a panel.
type Source func() (Snapshot, error)

// Handler performs a panel action and returns a status message.
type Handler func(Action) (string, error)

// Run owns only its pane's terminal. The header has no input controls; the
// member and task panes use tview's own application/event loop.
func Run(kind, current string, load Source, act Handler) error {
	if kind == "members" || kind == "tasks" {
		return runSidePanel(kind, current, load, act)
	}
	_, err := tea.NewProgram(headerModel{current: current, load: load, width: 24, height: 3}, tea.WithAltScreen()).Run()
	return err
}

type headerModel struct {
	current       string
	load          Source
	data          Snapshot
	width, height int
}

type headerSnapshot struct {
	data Snapshot
	err  error
}
type headerTick time.Time

// Init starts the header's snapshot read loop.
func (m headerModel) Init() tea.Cmd { return m.read }
func (m headerModel) read() tea.Msg { d, e := m.load(); return headerSnapshot{d, e} }

// Update applies size and snapshot messages to the header.
func (m headerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
	case headerSnapshot:
		if v.err == nil {
			m.data = v.data
			if !v.data.Active {
				return m, tea.Quit
			}
		}
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return headerTick(t) })
	case headerTick:
		return m, m.read
	}
	return m, nil
}

// View renders the current header snapshot.
func (m headerModel) View() string { return workspaceHeader(m.data.Team, m.current, m.width, m.height) }

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
