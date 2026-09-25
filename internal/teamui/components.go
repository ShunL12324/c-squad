package teamui

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Bubbles owns scrolling and clipping. Domain cards only supply content and
// translate their hit rectangles by the viewport's actual scroll position.
func (m model) taskViewport() viewport.Model {
	if m.detail && m.count() > 0 {
		rows := m.detailLines(m.detailBody(m.tasks()[m.selected]))
		for i := range rows {
			rows[i] = "   " + rows[i]
		}
		return m.contentViewport(rows)
	}
	rows, _ := m.taskContent()
	return m.contentViewport(rows)
}

func (m model) contentViewport(rows []string) viewport.Model {
	v := viewport.New(max(1, m.width), m.taskAvailable())
	v.SetContent(strings.Join(rows, "\n"))
	v.SetYOffset(m.viewport.YOffset)
	return v
}

func (m *model) scrollViewport(msg tea.KeyMsg) {
	if m.kind != "tasks" {
		return
	}
	v := m.taskViewport()
	v, _ = v.Update(msg)
	m.viewport = v
}

func (m model) memberFirst() int {
	if m.hasMaster() {
		return 1
	}
	return 0
}

type memberItem struct {
	member Member
	width  int
}

// FilterValue supplies the standard list item identity.
func (i memberItem) FilterValue() string { return i.member.ID }

// Title supplies the member name to the stock delegate.
func (i memberItem) Title() string {
	if i.member.ID == "master" {
		return "◆ " + clean(i.member.ID)
	}
	return clean(i.member.ID)
}

// Description supplies member metadata without owning item layout.
func (i memberItem) Description() string {
	member := i.member
	engine := member.Engine
	switch engine {
	case "codex":
		engine = "Codex"
	case "claude":
		engine = "Claude Code"
	}
	cwd := member.Cwd
	if cwd == "" {
		cwd = "Not recorded"
	}
	git := gitLine(member, max(1, i.width-6), canvas)
	if git == "" {
		git = "—"
	}
	task := "No assigned task"
	if member.Tasks != "" {
		task = "Task " + clean(member.Tasks)
	}
	return strings.Join([]string{engine + " · " + label(member.State), "Dir " + compactPath(cwd, max(1, i.width-10)), git, task, "", ""}, "\n")
}

// The standard delegate owns text truncation, item layout and selection styling.
// Our adapter supplies only per-member colors and the persistent session marker.
type memberDelegate struct {
	list.DefaultDelegate
	selected string
}

// Render applies member colors before using the standard item renderer.
func (d memberDelegate) Render(w io.Writer, model list.Model, index int, item list.Item) {
	i, ok := item.(memberItem)
	if !ok {
		return
	}
	color := i.member.Color
	if color == "" {
		color = accent
	}
	style := d.DefaultDelegate
	if i.member.ID == d.selected {
		style.Styles.NormalTitle = style.Styles.SelectedTitle.BorderForeground(lipgloss.Color(color))
		style.Styles.NormalDesc = style.Styles.SelectedDesc.BorderForeground(lipgloss.Color(color))
	}
	style.Styles.NormalTitle = style.Styles.NormalTitle.Foreground(lipgloss.Color(color)).Bold(true)
	style.Styles.SelectedTitle = style.Styles.NormalTitle
	style.Styles.SelectedDesc = style.Styles.NormalDesc
	style.Render(w, model, index, item)
}
func rosterDelegate(selected string) memberDelegate {
	d := list.NewDefaultDelegate()
	d.SetHeight(memberBlockRows - 1)
	d.SetSpacing(1)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(lipgloss.Color(muted))
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color(foreground))
	return memberDelegate{DefaultDelegate: d, selected: selected}
}
func (m model) memberList() list.Model {
	items := make([]list.Item, 0, max(0, m.count()-m.memberFirst()))
	for _, member := range m.data.Members[m.memberFirst():] {
		items = append(items, memberItem{member: member, width: m.width})
	}
	l := list.New(items, rosterDelegate(m.selectedID), max(1, m.width-4), m.rows()*memberBlockRows)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	l.Paginator.Type = paginator.Arabic
	l.Paginator.Page = min(m.pages.Page, max(0, l.Paginator.TotalPages-1))
	return l
}
