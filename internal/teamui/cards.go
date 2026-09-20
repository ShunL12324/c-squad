package teamui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// cardHit uses the same rendered bounds for pointer input and viewport selection.
type cardHit struct{ index, start, end, button int }

func (m model) taskCards() ([]string, []cardHit) {
	if len(m.tasks()) == 0 {
		if m.completed {
			return block([]string{textStyle("No completed tasks", foreground, true), "", textStyle("Finished work will appear here.", muted, false)}, m.width, surface, ""), nil
		}
		return block([]string{textStyle("Nothing in progress", foreground, true), "", textStyle("Tasks from Master appear here.", muted, false), textStyle("Past work is under Done.", muted, false)}, m.width, surface, ""), nil
	}
	available := max(0, m.height-taskHeaderRows-2)
	var lines []string
	var hits []cardHit
	for i := m.top; i < len(m.tasks()); i++ {
		card := m.taskCard(i)
		start := len(lines)
		lines = append(lines, card...)
		hits = append(hits, cardHit{i, start, len(lines), start + len(card) - 3})
	}
	offset := min(m.offset, max(0, len(lines)-available))
	end := min(len(lines), offset+available)
	lines = lines[offset:end]
	visible := hits[:0]
	for _, hit := range hits {
		if hit.end <= offset || hit.start >= end {
			continue
		}
		hit.start = max(0, hit.start-offset)
		hit.end = min(available, hit.end-offset)
		hit.button -= offset
		visible = append(visible, hit)
	}
	hits = visible
	return lines, hits
}

func (m model) taskCard(index int) []string {
	task := m.tasks()[index]
	selected := index == m.selected
	bg, stripe := surface, ""
	if selected {
		bg, stripe = selectedSurface, task.Color
	}
	width := max(1, m.width-6)
	wrap := func(text string, limit int) []string {
		rows := strings.Split(lipgloss.NewStyle().Width(width).Render(clean(text)), "\n")
		if len(rows) > limit {
			rows = rows[:limit]
			rows[limit-1] = line(rows[limit-1]+" …", width)
		}
		return rows
	}
	content := wrap(task.Title, 2)
	for i := range content {
		content[i] = textStyle(content[i], foreground, true)
	}
	owner := task.Owner
	if owner == "" {
		owner = "Unassigned"
	}
	content = append([]string{spread(textStyle(task.ID, accent, true), badge(task.State), width, bg), ""}, content...)
	content = append(content, textStyle(line("Owner: "+owner, width), muted, false))
	if task.Note != "" {
		content = append(content, textStyle(line(task.Note, width), "222", true))
	}
	progress := task.Progress
	content = append(content, "", textStyle(strings.Repeat("─", width), "240", false))
	content = append(content, milestoneLines(task.Milestones, width, 3)...)
	if progress != "" {
		content = append(content, "")
		content = append(content, textStyle("LATEST UPDATE", muted, true))
		content = append(content, wrap(progress, 2)...)
		content = append(content, "")
	}
	content = append(content, "", paint(" View details › ", accent, true))
	return block(content, m.width, bg, stripe)
}
