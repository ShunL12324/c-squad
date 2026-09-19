package teamui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const taskHeaderRows = 6
const taskTitleRow = 1
const taskFilterRow = 3

func (m Milestone) complete() bool {
	return m.State == "approved" || m.State == "reported" && !m.Gate
}

func milestoneLines(milestones []Milestone, width, limit int) []string {
	if len(milestones) == 0 {
		return []string{textStyle("No milestones defined", muted, false)}
	}
	full := limit < 0
	if full {
		limit = len(milestones)
	}
	done := 0
	for _, step := range milestones {
		if step.complete() {
			done++
		}
	}
	lines := []string{textStyle(fmt.Sprintf("MILESTONES  %d/%d", done, len(milestones)), muted, true)}
	for i, step := range milestones {
		if i >= limit {
			lines = append(lines, textStyle(fmt.Sprintf("+ %d more in details", len(milestones)-i), muted, false))
			break
		}
		mark, color := "○", muted
		if step.complete() {
			mark, color = "✓", accent
		} else if step.State == "awaiting_approval" || step.State == "reported" {
			mark, color = "◇", "222"
		}
		name := step.Name
		if mark == "◇" {
			name += " (approval needed)"
		}
		wrapped := strings.Split(lipgloss.NewStyle().Width(max(1, width-4)).Render(clean(name)), "\n")
		lines = append(lines, textStyle(mark+"  "+line(wrapped[0], max(1, width-3)), color, false))
		if i+1 < min(len(milestones), limit) {
			lines = append(lines, textStyle("│", "240", false))
		}
		// Overview stays compact; the full detail view preserves all step names.
		if full {
			for _, row := range wrapped[1:] {
				lines = append(lines, textStyle("    "+row, color, false))
			}
		}
	}
	return lines
}

func (m model) boardView() []string {
	if m.detail && m.count() > 0 {
		task := m.tasks()[m.selected]
		lines := []string{"", m.boardHeading("TASK DETAILS"), "", "  " + paint(" ‹ Back to tasks ", accent, true), "", ""}
		body := task.ID + " · " + label(task.State) + "\n\n" + task.Title + "\n\nOwner: " + task.Owner + "\n\n"
		body += strings.Join(milestoneLines(task.Milestones, max(1, m.width-4), -1), "\n")
		body += "\n\n" + task.Detail
		return append(lines, m.details(body, m.height-taskHeaderRows-2)...)
	}
	lines := []string{"", m.boardHeading("TASKS"), "", m.taskFilters(), "", ""}
	cards, _ := m.taskCards()
	return append(lines, cards...)
}

func (m model) boardHeading(title string) string {
	return spread(textStyle("  "+title, foreground, true), paint(" × ", foreground, true), max(1, m.width-2), canvas) + "  "
}

// tasks keeps all task interactions in the same filtered index space.
func (m model) tasks() []Task {
	var tasks []Task
	for _, task := range m.data.Tasks {
		if (task.State == "done") == m.completed {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func (m *model) filterTasks(completed bool) {
	if m.completed == completed {
		return
	}
	m.completed = completed
	m.selected, m.top, m.offset = 0, 0, 0
	m.detail = false
	m.remember()
}

// filterSplit is shared by the renderer and the mouse hit test so the painted
// boundary and the clickable boundary can never drift apart. The right segment
// absorbs the odd column so the pair exactly spans the card content width.
func (m model) filterSplit() (int, int) {
	left := max(1, (m.width-4)/2)
	return left, max(1, m.width-4-left)
}

// filterSegment paints padding and label in one style; unstyled padding would
// punch canvas-coloured holes either side of the centred label.
func filterSegment(text string, width int, selected bool) string {
	style := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).
		Background(lipgloss.Color(surface)).Foreground(lipgloss.Color(muted))
	if selected {
		style = style.Background(lipgloss.Color(accent)).Foreground(lipgloss.Color(canvas)).Bold(true)
	}
	return style.Render(line(text, width))
}

func (m model) taskFilters() string {
	done := 0
	for _, task := range m.data.Tasks {
		if task.State == "done" {
			done++
		}
	}
	left, right := m.filterSplit()
	// Paint the gutters too; cells emitted after the segments' reset would
	// otherwise inherit the terminal's own background instead of the canvas.
	gutter := lipgloss.NewStyle().Background(lipgloss.Color(canvas)).Render("  ")
	return gutter + filterSegment(fmt.Sprintf("Active %d", len(m.data.Tasks)-done), left, !m.completed) +
		filterSegment(fmt.Sprintf("Done %d", done), right, m.completed) + gutter
}
