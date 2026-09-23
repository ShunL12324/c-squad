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
// Branch and Commit describe the member's own working directory, which need not
// be the repository its task is bound to; Commit is set only on a detached HEAD.
type Member struct {
	ID, Engine, State, Color, Tasks, Cwd string
	Branch, Commit                       string
	Worktree                             bool
}

// Task is a presentation snapshot of a task and its delivery evidence.
// Note carries a terse qualifier for a done task that was not merged, so the card
// never reads identically to a merged one. Completion is set only for work the
// agent workflow accepted; the panel never marks completion itself.
type Task struct {
	ID, Title, State, Owner, Color, Progress, Detail, Note string
	Completion                                             string
	Milestones                                             []Milestone
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
type Action struct{ Kind, Member, Task string }

// Source reads snapshots without refreshing or mutating agent state.
type Source func() (Snapshot, error)

// Handler performs an action outside the renderer, returning what to tell the
// user about it. The renderer still owns no team state: it emits an intent and
// renders the answer.
type Handler func(Action) (string, error)

type snapshotMsg struct {
	data Snapshot
	err  error
}
type actionMsg struct {
	kind, task, note string
	err              error
	quit             bool
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
	briefs                               map[string]briefFeedback
}

// Run owns only its pane's terminal; the native agent continues in another pane.
func Run(kind, current string, load Source, act Handler) error {
	// These panels run inside tmux, where color identifies members and states.
	// Agent launchers may export NO_COLOR for their own captured CLI output;
	// do not let that inherited setting disable the interactive panel palette.
	if lipgloss.ColorProfile() != termenv.TrueColor {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}

	m := model{kind: kind, current: current, load: load, act: act, width: 24, height: 24, loading: true, briefs: map[string]briefFeedback{}}
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
	return func() tea.Msg {
		note, err := m.act(a)
		return actionMsg{kind: a.Kind, task: a.Task, note: note, err: err, quit: a.Kind == "close"}
	}
}

// brief asks Master to summarise the selected task. It reports nothing about the
// task itself: the request is a message, and the task is untouched by it.
func (m model) brief() (tea.Model, tea.Cmd) {
	if m.kind != "tasks" || m.count() == 0 {
		return m, nil
	}
	if m.briefs == nil {
		m.briefs = map[string]briefFeedback{}
	}
	id := m.tasks()[m.selected].ID
	if !m.press(id) {
		return m, nil
	}
	return m, m.action(Action{Kind: "brief", Task: id})
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
	if m.kind == "members" {
		first := 0
		if m.hasMaster() {
			first = 1
		}
		// A larger pane can fit cards that were above the old viewport.
		// Clamp before the pinned-Master return as well as for other members.
		m.top = max(first, min(m.top, max(first, m.count()-m.rows())))
		if m.hasMaster() && m.selected == 0 {
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
		// A larger pane or wider wrapping lowers the last page.
		m.offset = min(m.offset, m.maxOffset())
	case snapshotMsg:
		return m.updateSnapshot(v)
	case tickMsg:
		return m, m.read
	case actionMsg:
		m.err = v.err
		if v.kind == "brief" {
			// Keep the outcome on the card it belongs to. The footer shows one
			// error at a time and the user may have moved on already.
			m.err = nil
			f := briefFeedback{phase: briefDone, text: v.note, at: time.Now()}
			if v.err != nil {
				f = briefFeedback{phase: briefFailed, text: v.err.Error(), at: time.Now()}
			}
			if m.briefs == nil {
				m.briefs = map[string]briefFeedback{}
			}
			m.briefs[v.task] = f
		}
		if v.quit && v.err == nil {
			return m, tea.Quit
		}
	case tea.KeyMsg:
		return m.updateKey(v)
	case tea.MouseMsg:
		return m.updateMouse(v)

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
	m.offset = max(0, min(m.offset, m.maxOffset())+3*direction)
	m.offset = min(m.offset, m.maxOffset())
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

// detailLines wraps detail text to the panel, so scrolling bounds and the
// renderer count the same lines.
func (m model) detailLines(text string) []string {
	return strings.Split(lipgloss.NewStyle().Width(max(1, m.width-6)).Render(clean(text)), "\n")
}

func (m model) details(text string, available int) []string {
	if available < 1 {
		return nil
	}
	lines := m.detailLines(text)
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
