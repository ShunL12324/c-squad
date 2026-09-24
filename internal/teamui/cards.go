package teamui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// cardButton is one rendered action and the cells that trigger it. row is a line
// index, resolved to the same coordinate space as cardHit.start by whoever holds
// the button.
type cardButton struct {
	action     string
	row        int
	start, end int
}

// cardHit uses the same rendered bounds for pointer input and viewport selection.
type cardHit struct {
	index, start, end int
	buttons           []cardButton
}

// cardContentX is the first screen column of card content: block paints two
// gutter cells, then one cell of its own horizontal padding.
const cardContentX = 3

const detailsLabel = " View details › "

func (m model) taskCards() ([]string, []cardHit) {
	if len(m.tasks()) == 0 {
		if m.completed {
			return block([]string{textStyle("No finished tasks", foreground, true), "", textStyle("Done and cancelled work appears here.", muted, false)}, m.width, surface, ""), nil
		}
		return block([]string{textStyle("Nothing in progress", foreground, true), "", textStyle("Tasks from Master appear here.", muted, false), textStyle("Past work is under Done or cancelled.", muted, false)}, m.width, surface, ""), nil
	}
	available := max(0, m.height-taskHeaderRows-2)
	var lines []string
	var hits []cardHit
	for i := m.top; i < len(m.tasks()); i++ {
		card, buttons := m.taskCard(i)
		start := len(lines)
		lines = append(lines, card...)
		for j := range buttons {
			buttons[j].row += start
		}
		hits = append(hits, cardHit{index: i, start: start, end: len(lines), buttons: buttons})
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
		kept := hit.buttons[:0]
		for _, button := range hit.buttons {
			button.row -= offset
			// A button scrolled out of the viewport must stop being clickable,
			// or its cells would trigger whatever row now occupies them.
			if button.row < 0 || button.row >= available {
				continue
			}
			kept = append(kept, button)
		}
		hit.buttons = kept
		visible = append(visible, hit)
	}
	hits = visible
	return lines, hits
}

// taskCardButtons lays out the card actions and the cells that trigger them.
// The renderer and the mouse hit test read this one result, so a painted button
// and its clickable region cannot drift apart - the discipline filterSplit
// already applies to the task filter.
func taskCardButtons(contentWidth int) ([]string, []cardButton) {
	details := ansi.StringWidth(detailsLabel)
	// block truncates an overlong row, so never claim cells beyond the content
	// it can actually paint.
	return []string{paint(detailsLabel, accent, true)}, []cardButton{
		{action: "details", row: 0, start: cardContentX, end: min(cardContentX+details, cardContentX+contentWidth)},
	}
}

// taskCard returns the rendered card and its buttons, whose rows are indices
// into the returned slice.
func (m model) taskCard(index int) ([]string, []cardButton) {
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
	if task.Completion != "" {
		content = append(content, textStyle(line(task.Completion, width), "115", true))
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
	content = append(content, "")
	rows, buttons := taskCardButtons(width)
	base := len(content)
	content = append(content, rows...)
	card := block(content, m.width, bg, stripe)
	if len(card) == len(content) {
		// block declines to frame a pane this narrow and returns the content
		// unpadded, so there is no leading row to account for and nothing
		// worth clicking either.
		return card, nil
	}
	for j := range buttons {
		buttons[j].row += base + 1
	}
	return card, buttons
}
