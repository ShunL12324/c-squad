package teamui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// CanvasColor is shared with the native tmux pane background.
const CanvasColor = "234"

const (
	brandSurface = "236"
	foreground   = "253"
	muted        = "245"
	accent       = "115"
)

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

func line(s string, width int) string {
	return ansi.Truncate(strings.ReplaceAll(clean(s), "\n", " "), max(1, width), "…")
}

func spread(left, right string, width int, background string) string {
	right = ansi.Truncate(right, max(1, width), "…")
	left = ansi.Truncate(left, max(0, width-ansi.StringWidth(right)-1), "…")
	gap := strings.Repeat(" ", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right)))
	return left + lipgloss.NewStyle().Background(lipgloss.Color(background)).Render(gap) + right
}

// workspaceHeader occupies its own full-width tmux pane above the workspace.
func workspaceHeader(team, current string, width, height int) string {
	headerText := func(text, color string, bold bool) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(brandSurface)).Foreground(lipgloss.Color(color)).Bold(bold).Render(text)
	}
	left := headerText("  >_  C SQUAD", accent, true) + headerText("   /   ", muted, false) + headerText(clean(team), foreground, true)
	right := headerText(line(current, max(1, width/3))+"  ", muted, false)
	style := lipgloss.NewStyle().Background(lipgloss.Color(brandSurface)).Foreground(lipgloss.Color(foreground))
	rows := []string{"", spread(left, right, width, brandSurface), ""}
	for i, row := range rows {
		row = ansi.Truncate(row, width, "")
		rows[i] = style.Render(row + strings.Repeat(" ", max(0, width-ansi.StringWidth(row))))
	}
	return strings.Join(rows[:min(len(rows), height)], "\n")
}
