package teamui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// cardHit uses the same rendered bounds for pointer input and viewport selection.
type cardHit struct{ index, start, end int }

func (m model) taskCards() ([]string, []cardHit) {
	if len(m.data.Tasks) == 0 {
		return []string{" No tasks yet.", " Ask Master to plan work."}, nil
	}
	available := max(0, m.height-4)
	var lines []string
	var hits []cardHit
	for i := m.top; i < len(m.data.Tasks) && len(lines) < available; i++ {
		card := m.taskCard(i)
		// Keep subsequent cards whole so their titles and metadata stay together.
		if len(lines) > 0 && len(lines)+len(card) > available {
			break
		}
		start := len(lines)
		lines = append(lines, card[:min(len(card), available-start)]...)
		hits = append(hits, cardHit{i, start, len(lines)})
	}
	return lines, hits
}

func (m model) taskCard(index int) []string {
	task := m.data.Tasks[index]
	selected := index == m.selected
	width := max(1, m.width-4)
	wrap := func(text string, limit int) []string {
		rows := strings.Split(lipgloss.NewStyle().Width(width).Render(clean(text)), "\n")
		if len(rows) > limit {
			rows = rows[:limit]
			rows[limit-1] = line(rows[limit-1]+" …", width)
		}
		return rows
	}
	content := wrap(task.ID+"  "+task.Title, 2)
	for i := range content {
		content[i] = paint(content[i], task.Color, selected)
	}
	owner := task.Owner
	if owner == "" {
		owner = "Unassigned"
	}
	content = append(content, paint(line(task.State, width), task.Color, false), line("Owner: "+owner, width), "")
	progress := task.Progress
	if progress == "" {
		progress = "No progress update yet."
	}
	content = append(content, wrap(progress, 2)...)
	if selected && task.Detail != "" {
		content = append(content, "", paint("DETAILS", task.Color, true))
		rows := strings.Split(lipgloss.NewStyle().Width(width).Render(clean(task.Detail)), "\n")
		count := max(1, min(7, m.height-len(content)-7))
		offset := min(m.offset, max(0, len(rows)-count))
		content = append(content, rows[offset:min(len(rows), offset+count)]...)
	}
	if m.width < 6 {
		return content
	}
	style := lipgloss.NewStyle().Width(m.width-2).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	if selected {
		style = style.BorderForeground(lipgloss.Color(task.Color))
	}
	return strings.Split(style.Render(strings.Join(content, "\n")), "\n")
}
