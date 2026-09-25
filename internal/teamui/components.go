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
	member  Member
	width   int
	current bool
}

// FilterValue supplies the standard list item identity.
func (i memberItem) FilterValue() string { return i.member.ID }

// Title supplies the member name to the stock delegate.
func (i memberItem) Title() string {
	name := clean(i.member.ID)
	if i.member.ID == "master" {
		name = "◆ " + name
	}
	if i.current {
		name = "● " + name
	}
	return line(name, max(1, i.width-6))
}

// Description restores the card's information hierarchy while the stock
// delegate still owns item height, clipping, cursor styling and pagination.
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
	width := max(1, i.width-6)
	bg := surface
	if i.current {
		bg = selectedSurface
	}
	git := gitLine(member, width, bg)
	if git == "" {
		git = textStyle(strings.Repeat("─", width), "240", false)
	}
	task := textStyle("No assigned task", muted, false)
	if member.Tasks != "" {
		color := member.Color
		if color == "" {
			color = accent
		}
		task = spread(textStyle("TASK", muted, false), textStyle(line(member.Tasks, max(1, width-6)), color, true), width, bg)
	}
	meta := spread(textStyle(engine, muted, false), badge(member.State), width, bg)
	path := "Dir " + compactPath(cwd, max(1, width-4))
	if width < 12 {
		// A narrow pane cannot fit both columns or a padded badge. Keep the
		// values visible instead of truncating every row to its field label.
		meta = label(member.State)
		path = compactPath(cwd, width)
		if member.Branch != "" {
			git = tail(member.Branch, width)
		} else if member.Commit != "" {
			git = tail(member.Commit, width)
		}
		if member.Tasks != "" {
			task = line(member.Tasks, width)
		} else {
			task = "—"
		}
	}
	return strings.Join([]string{meta, path, git, task, "", ""}, "\n")
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
	bg := surface
	if i.current {
		bg = selectedSurface
	}
	cardWidth := max(1, i.width-4)
	style := d.DefaultDelegate
	style.Styles.NormalTitle = style.Styles.NormalTitle.Width(cardWidth).
		Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(color)).Bold(true)
	style.Styles.NormalDesc = style.Styles.NormalDesc.Width(cardWidth).
		Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(muted))
	style.Styles.SelectedTitle = style.Styles.SelectedTitle.Width(max(1, cardWidth-1)).
		Background(lipgloss.Color(bg)).BorderForeground(lipgloss.Color(color)).
		Foreground(lipgloss.Color(color)).Bold(true)
	style.Styles.SelectedDesc = style.Styles.SelectedDesc.Width(max(1, cardWidth-1)).
		Background(lipgloss.Color(bg)).BorderForeground(lipgloss.Color(color)).
		Foreground(lipgloss.Color(muted))
	if i.width < 18 {
		style.Styles.NormalTitle = style.Styles.NormalTitle.PaddingLeft(1)
		style.Styles.NormalDesc = style.Styles.NormalDesc.PaddingLeft(1)
		style.Styles.SelectedTitle = style.Styles.SelectedTitle.PaddingLeft(0)
		style.Styles.SelectedDesc = style.Styles.SelectedDesc.PaddingLeft(0)
	}
	if i.member.ID == d.selected {
		// Mouse wheel pages independently of the keyboard cursor. A rebuilt
		// list can show the selected member on a page other than its native
		// cursor index, so apply the stock selected styles by stable member ID.
		style.Styles.NormalTitle = style.Styles.SelectedTitle
		style.Styles.NormalDesc = style.Styles.SelectedDesc
	} else {
		// The pinned Master is a one-item list whose native index is zero.
		style.Styles.SelectedTitle = style.Styles.NormalTitle
		style.Styles.SelectedDesc = style.Styles.NormalDesc
	}
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
		items = append(items, memberItem{member: member, width: m.width, current: member.ID == m.current})
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

// moveMember lets the standard list handle cursor bounds and page transitions.
// Only the separately pinned Master needs an application-level boundary.
func (m *model) moveMember(delta int) {
	if m.count() == 0 {
		return
	}
	first := m.memberFirst()
	if first == 1 && (m.selected == 0 || m.selected == 1 && delta < 0) {
		if delta > 0 && m.count() > 1 {
			m.selected = 1
		} else {
			m.selected = 0
		}
		m.remember()
		m.reveal()
		return
	}
	l := m.memberList()
	l.Select(max(0, m.selected-first))
	direction := tea.KeyDown
	if delta < 0 {
		direction = tea.KeyUp
	}
	l, _ = l.Update(tea.KeyMsg{Type: direction})
	m.selected = l.Index() + first
	m.pages = l.Paginator
	m.remember()
}
