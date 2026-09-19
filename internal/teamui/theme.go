package teamui

import (
	"strings"

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
	appendCard := func(i int) {
		start := len(rows)
		rows = append(rows, m.memberCard(i)...)
		hits = append(hits, cardHit{index: i, start: start, end: len(rows) - 1})
	}
	first := m.top
	if m.hasMaster() {
		rows = append(rows, textStyle("  LEAD", muted, true), "")
		appendCard(0)
		rows = append(rows, textStyle("  MEMBERS", muted, true), "")
		first = max(1, first)
	} else {
		rows = append(rows, textStyle("  MEMBERS", muted, true), "")
	}
	for i := first; i < min(m.count(), first+m.rows()); i++ {
		appendCard(i)
	}
	return rows, hits
}

func (m model) hasMaster() bool {
	return len(m.data.Members) > 0 && m.data.Members[0].ID == "master"
}

func (m model) memberBlocks() []string { rows, _ := m.memberCards(); return rows }

func (m model) memberCard(i int) []string {
	member := m.data.Members[i]
	bg, stripe := surface, ""
	if member.ID == m.current {
		bg = selectedSurface
		stripe = member.Color
	}
	color := member.Color
	if color == "" {
		color = accent
	}
	name := line(member.ID, m.width-6)
	if member.ID == "master" {
		name = "◆ " + line(member.ID, m.width-8)
		stripe = color
	}

	engineName := member.Engine
	switch engineName {
	case "claude":
		engineName = "Claude Code"
	case "codex":
		engineName = "Codex"
	}
	width := max(1, m.width-6)
	meta := spread(textStyle(engineName, muted, false), badge(member.State), width, bg)
	cwd := member.Cwd
	if cwd == "" {
		cwd = "Not recorded"
	}
	cwd = "Dir " + compactPath(cwd, max(1, width-4))
	task := textStyle("No assigned task", muted, false)
	if member.Tasks != "" {
		color := member.Color
		if color == "" {
			color = accent
		}
		task = spread(textStyle("TASK", muted, false), textStyle(line(member.Tasks, max(1, width-6)), color, true), width, bg)
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).
		Underline(i == m.selected && member.ID != m.current).Render(name)
	return block([]string{title, meta, textStyle(cwd, muted, false),
		textStyle(strings.Repeat("─", width), "240", false), task}, m.width, bg, stripe)

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
