package teamui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Completion comes from the snapshot; the panel offers no way to mark it.
func TestCompletionIsDisplayedNotClicked(t *testing.T) {
	for _, width := range []int{28, 40, 80} {
		var acted []Action
		m := briefModel(width, func(a Action) (string, error) { acted = append(acted, a); return "", nil })
		m.data.Tasks[0].State = "done"
		m.completed = true
		lines, hits := m.taskCards()
		if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Completed") {
			t.Fatal("unmarked task shows completion")
		}
		m.data.Tasks[0].Completion = "✓ Completed · merged abc1234"
		lines, hits = m.taskCards()
		if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "✓ Completed") {
			t.Fatalf("width %d: completion missing", width)
		}
		for _, hit := range hits {
			for _, button := range hit.buttons {
				if button.action != "details" && button.action != "brief" {
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
