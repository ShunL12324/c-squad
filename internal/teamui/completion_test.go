package teamui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func taskModel(width int, act Handler) model {
	return model{kind: "tasks", width: width, height: 60, act: act,
		data: Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Wire the panel", Owner: "a", State: "in progress"}}}}
}

// The Brief report button and its b/r keys are gone: no card, detail view or
// footer offers it, and the keys send nothing.
func TestPanelOffersNoBriefReport(t *testing.T) {
	for _, width := range []int{28, 40, 80} {
		var acted []Action
		m := taskModel(width, func(a Action) (string, error) { acted = append(acted, a); return "", nil })
		lines, hits := m.taskCards()
		if text := ansi.Strip(strings.Join(append(lines, m.footer()...), "\n")); strings.Contains(text, "Brief") {
			t.Fatalf("width %d: card or footer offers Brief:\n%s", width, text)
		}
		for _, hit := range hits {
			for _, button := range hit.buttons {
				if button.action != "details" {
					t.Fatalf("width %d: unexpected card button %q", width, button.action)
				}
			}
		}
		for _, key := range []rune{'b', 'r'} {
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}); cmd != nil {
				cmd()
			}
		}
		m.detail = true
		if text := ansi.Strip(m.View()); strings.Contains(text, "Brief") {
			t.Fatalf("width %d: detail view offers Brief:\n%s", width, text)
		}
		for _, key := range []rune{'b', 'r'} {
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}); cmd != nil {
				cmd()
			}
		}
		if len(acted) != 0 {
			t.Fatalf("width %d: brief keys still act: %+v", width, acted)
		}
	}
}

// Completion comes from the snapshot; the panel offers no way to mark it.
func TestCompletionIsDisplayedNotClicked(t *testing.T) {
	for _, width := range []int{28, 40, 80} {
		var acted []Action
		m := taskModel(width, func(a Action) (string, error) { acted = append(acted, a); return "", nil })
		m.data.Tasks[0].State = "done"
		m.completed = true
		lines, _ := m.taskCards()
		if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Completed") {
			t.Fatal("unmarked task shows completion")
		}
		m.data.Tasks[0].Completion = "✓ Completed · merged abc1234"
		lines, hits := m.taskCards()
		if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "✓ Completed") {
			t.Fatalf("width %d: completion missing", width)
		}
		for _, hit := range hits {
			for _, button := range hit.buttons {
				if button.action != "details" {
					t.Fatalf("unexpected card button %q", button.action)
				}
			}
		}
		if !strings.Contains(m.detailBody(m.data.Tasks[0]), "✓ Completed · merged abc1234") {
			t.Fatal("details omit completion")
		}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		if cmd != nil {
			cmd()
		}
		if len(acted) != 0 || strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "Confirm") {
			t.Fatalf("panel still confirms: %+v", acted)
		}
	}
}
