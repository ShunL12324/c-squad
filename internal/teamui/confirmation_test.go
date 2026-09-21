package teamui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmButtonAndKeyboardAreSeparateFromBrief(t *testing.T) {
	for _, width := range []int{28, 40, 80} {
		var got Action
		m := briefModel(width, func(a Action) (string, error) { got = a; return "confirmed", nil })
		m.data.Tasks[0].CanConfirm = true
		_, hits := m.taskCards()
		found := false
		for _, hit := range hits {
			for _, button := range hit.buttons {
				if button.action != "confirm" {
					continue
				}
				found = true
				_, cmd := m.Update(tea.MouseMsg{X: button.start, Y: button.row + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				if cmd == nil {
					t.Fatal("confirmation click ignored")
				}
				cmd()
				if got.Kind != "confirm" || got.Task != "T1" {
					t.Fatalf("wrong action: %+v", got)
				}
			}
		}
		if !found {
			t.Fatal("missing confirmation button")
		}
		m.data.Tasks[0].CanConfirm = false
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		if cmd != nil {
			t.Fatal("confirmed or unfinished task accepted confirmation")
		}
	}
}
