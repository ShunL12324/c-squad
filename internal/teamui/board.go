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

func (m model) taskFilters() string {
	done := 0
	for _, task := range m.data.Tasks {
		if task.State == "done" {
			done++
		}
	}
	width := max(1, (m.width-4)/2)
	left := fmt.Sprintf(" Active %d", len(m.data.Tasks)-done)
	right := fmt.Sprintf(" Done %d", done)
	return "  " + paint(fmt.Sprintf("%-*s", width, line(left, width)), accent, !m.completed) + paint(fmt.Sprintf("%-*s", max(1, m.width-4-width), line(right, max(1, m.width-4-width))), accent, m.completed)
}
