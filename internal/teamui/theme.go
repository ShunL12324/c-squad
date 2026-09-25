package teamui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// CanvasColor is the 256-color background shared by rendering and tmux panes.
const CanvasColor = "234"

// Panel surfaces deliberately differ from the native terminal's own theme.
const (
	canvas           = CanvasColor
	surface          = "235"
	selectedSurface  = "237"
	brandSurface     = "236"
	foreground       = "253"
	muted            = "245"
	accent           = "115"
	memberHeaderRows = 3
	memberBlockRows  = 8
)

func textStyle(text, color string, bold bool) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(bold).Render(text)
}

func label(state string) string {
	switch strings.ReplaceAll(state, "_", " ") {
	case "in progress", "working":
		return "Working"
	case "idle":
		return "Idle"
	case "done":
		return "Done"
	case "blocked":
		return "Blocked"
	case "ready":
		return "Ready"
	case "in review", "review":
		return "Review"
	default:
		state = strings.ReplaceAll(state, "_", " ")
		if state == "" {
			return "Pending"
		}
		return strings.ToUpper(state[:1]) + state[1:]
	}
}

func badge(state string) string {
	color, bg := "153", "238"
	switch label(state) {
	case "Done", "Ready":
		color, bg = "115", "238"
	case "Blocked":
		color, bg = "222", "238"
	case "Cancelled":
		color, bg = "210", "238"
	case "Idle", "Pending", "Waiting":
		color, bg = muted, "239"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Background(lipgloss.Color(bg)).Render(" " + label(state) + " ")
}

// block paints every cell, including padding, so cards remain rectangular.
func block(content []string, width int, bg, stripe string) []string {
	if width < 8 {
		return content
	}
	rows := append([]string{""}, content...)
	rows = append(rows, "")
	style := lipgloss.NewStyle().Width(width-4).Padding(0, 1).
		Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(foreground))
	result := make([]string, 0, len(rows)+1)
	gutter := lipgloss.NewStyle().Background(lipgloss.Color(CanvasColor))
	edge := gutter.Render("  ")
	if stripe != "" {
		edge = gutter.Foreground(lipgloss.Color(stripe)).Render(" ▎")
	}
	for _, row := range rows {
		result = append(result, edge+style.Render(ansi.Truncate(row, width-6, "…"))+gutter.Render("  "))
	}
	return append(result, "")
}

// memberCards pins Master above the independently scrolling member list.
func (m model) memberCards() ([]string, []cardHit) {
	rows := []string{""}
	var hits []cardHit
	if m.hasMaster() {
		rows = append(rows, textStyle("  LEAD", muted, true), "")
		start := len(rows)
		rows = append(rows, m.memberCard(0)...)
		hits = append(hits, cardHit{index: 0, start: start, end: len(rows) - 1})
	}
	l := m.memberList()
	heading := "  MEMBERS"
	if l.Paginator.TotalPages > 1 {
		heading += "  " + l.Paginator.View()
	}
	rows = append(rows, textStyle(heading, muted, true), "")
	start := len(rows)
	if len(l.Items()) == 0 {
		return rows, hits
	}
	for _, row := range strings.Split(l.View(), "\n") {
		rows = append(rows, "  "+row)
	}
	first, last := l.Paginator.GetSliceBounds(len(l.Items()))
	for i := first; i < last; i++ {
		top := start + (i-first)*memberBlockRows
		hits = append(hits, cardHit{index: i + m.memberFirst(), start: top, end: top + memberBlockRows - 1})
	}
	return rows, hits
}

func (m model) hasMaster() bool {
	return len(m.data.Members) > 0 && m.data.Members[0].ID == "master"
}

func (m model) memberBlocks() []string { rows, _ := m.memberCards(); return rows }

func (m model) memberCard(i int) []string {
	var out strings.Builder
	item := memberItem{member: m.data.Members[i], width: m.width}
	l := list.New([]list.Item{item}, rosterDelegate(m.selectedID), max(1, m.width-4), memberBlockRows)
	rosterDelegate(m.selectedID).Render(&out, l, 0, item)
	rows := strings.Split(out.String(), "\n")
	for len(rows) < memberBlockRows-1 {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = "  " + rows[i]
	}
	return append(rows, "")
}

// gitLine names the branch of the member's OWN directory. Branch names are
// hierarchical and their distinguishing segment is last, so an overlong one
// keeps its tail, the way compactPath keeps a path's trailing components.
func gitLine(member Member, width int, bg string) string {
	value := member.Branch
	if value == "" && member.Commit != "" {
		value = "detached " + member.Commit
	}
	if value == "" {
		return ""
	}
	const label = "Git "
	room := max(1, width-len(label))
	// A linked worktree is always marked: which checkout a member sits in is the
	// confusion this line exists to remove, and a truncated branch still shows
	// the segment that identifies it.
	if member.Worktree && room > 4 {
		left := textStyle(label, muted, false) + textStyle(tail(value, room-3), foreground, false)
		return spread(left, textStyle("wt", muted, false), width, bg)
	}
	return textStyle(label, muted, false) + textStyle(tail(value, room), foreground, false)
}

func tail(s string, width int) string {
	s = clean(s)
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.TruncateLeft(s, ansi.StringWidth(s)-width+1, "…")
}

// spread anchors metadata to both edges without letting long labels wrap.
func spread(left, right string, width int, bg string) string {
	right = ansi.Truncate(right, max(1, width), "…")
	left = ansi.Truncate(left, max(0, width-ansi.StringWidth(right)-1), "…")
	gap := strings.Repeat(" ", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right)))
	return left + lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(gap) + right
}

// compactPath keeps complete trailing components wherever space permits.
func compactPath(path string, width int) string {
	path = clean(path)
	if ansi.StringWidth(path) <= width {
		return path
	}
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		tail := "…/" + strings.Join(parts[i:], "/")
		if ansi.StringWidth(tail) <= width {
			return tail
		}
	}
	return ansi.TruncateLeft(path, ansi.StringWidth(path)-width+1, "…")
}

// workspaceHeader occupies its own full-width tmux pane above the workspace.
func (m model) workspaceHeader() string {
	headerText := func(text, color string, bold bool) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(brandSurface)).Foreground(lipgloss.Color(color)).Bold(bold).Render(text)
	}
	left := headerText("  >_  C SQUAD", accent, true) + headerText("   /   ", muted, false) + headerText(clean(m.data.Team), foreground, true)
	right := headerText(line(m.current, max(1, m.width/3))+"  ", muted, false)
	style := lipgloss.NewStyle().Background(lipgloss.Color(brandSurface)).Foreground(lipgloss.Color(foreground))
	rows := []string{"", spread(left, right, m.width, brandSurface), ""}
	for i, row := range rows {
		row = ansi.Truncate(row, m.width, "")
		rows[i] = style.Render(row + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(row))))
	}
	return strings.Join(rows[:min(len(rows), m.height)], "\n")
}
