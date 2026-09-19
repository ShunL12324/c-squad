package teamui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionSurvivesTaskUpdates(t *testing.T) {
	m := model{kind: "tasks", tab: "tasks", height: 24, width: 36, selectedID: "T2", data: Snapshot{Active: true, Tasks: []Task{{ID: "T1"}, {ID: "T2"}}}}
	next, _ := m.Update(snapshotMsg{data: Snapshot{Active: true, Tasks: []Task{{ID: "T2"}, {ID: "T3"}}}})
	got := next.(model)
	if got.selected != 0 || got.selectedID != "T2" {
		t.Fatalf("selection changed: %+v", got)
	}
	next, _ = got.Update(snapshotMsg{data: Snapshot{Active: true}})
	if next.(model).count() != 0 {
		t.Fatal("removed task remained visible")
	}
}
func TestMouseSelectsTaskWithoutOpeningTerminal(t *testing.T) {
	m := model{kind: "tasks", tab: "tasks", height: 30, width: 36, data: Snapshot{Active: true, Tasks: []Task{{ID: "T1"}, {ID: "T2"}}}}
	next, cmd := m.Update(tea.MouseMsg{X: 4, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil || next.(model).selectedID != "T2" {
		t.Fatal("task click should only select its details")
	}
}
func TestViewsFitSmallAndUnicodeTerminals(t *testing.T) {
	for _, kind := range []string{"members", "tasks"} {
		for _, size := range [][2]int{{1, 1}, {12, 8}, {24, 30}, {36, 40}} {
			m := model{kind: kind, tab: "tasks", width: size[0], height: size[1], data: Snapshot{Active: true, Team: "Example", Members: []Member{{ID: "dev", Color: "121", Tasks: "T1"}}, Tasks: []Task{{ID: "T1", Title: "界面 👩‍💻 long task title", Detail: strings.Repeat("progress with evidence\n", 30), Color: "121"}}}}
			lines := strings.Split(m.View(), "\n")
			if len(lines) > size[1] {
				t.Fatal("overflowing height")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("overflowing width: %q", line)
				}
			}
		}
	}
}
