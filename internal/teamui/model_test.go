package teamui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionSurvivesTaskUpdates(t *testing.T) {
	m := model{kind: "tasks", height: 24, width: 36, selectedID: "T2", data: Snapshot{Active: true, Tasks: []Task{{ID: "T1"}, {ID: "T2"}}}}
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
	m := model{kind: "tasks", height: 50, width: 36, data: Snapshot{Active: true, Tasks: []Task{{ID: "T1"}, {ID: "T2"}}}}
	_, hits := m.taskCards()
	next, cmd := m.Update(tea.MouseMsg{X: 4, Y: hits[1].start + taskHeaderRows + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil || next.(model).selectedID != "T2" {
		t.Fatal("task click should only select its details")
	}
}
func TestViewsFitSmallAndUnicodeTerminals(t *testing.T) {
	for _, kind := range []string{"members", "tasks"} {
		for _, size := range [][2]int{{1, 1}, {12, 8}, {24, 30}, {36, 40}} {
			m := model{kind: kind, width: size[0], height: size[1], data: Snapshot{Active: true, Team: "Example", Members: []Member{{ID: "dev", Color: "121", Tasks: "T1"}}, Tasks: []Task{{ID: "T1", Title: "界面 👩‍💻 long task title", Detail: strings.Repeat("progress with evidence\n", 30), Color: "121"}}}}
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

func TestCardsUseRenderedBoundsForSelection(t *testing.T) {
	m := model{kind: "tasks", width: 36, height: 40, data: Snapshot{Active: true, Tasks: []Task{
		{ID: "T1", Title: "A title that wraps across multiple lines", State: "in progress", Owner: "dev", Detail: strings.Repeat("Evidence\n", 20)},
		{ID: "T2", Title: "Test the change", State: "ready", Owner: "tester"},
		{ID: "T3", Title: "Review", State: "ready"},
	}}}
	_, hits := m.taskCards()
	if len(hits) < 2 {
		t.Fatal("second card not visible")
	}
	next, cmd := m.Update(tea.MouseMsg{X: 4, Y: hits[1].start + taskHeaderRows + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	selected := next.(model)
	if cmd != nil || selected.selectedID != "T2" {
		t.Fatal("wrapped card changed pointer target")
	}
	for range 3 {
		selected.move(1)
	}
	_, hits = selected.taskCards()
	if len(hits) == 0 || hits[len(hits)-1].index != 2 {
		t.Fatal("keyboard selection scrolled out of view")
	}
	view := ansi.Strip(selected.View())
	if !strings.Contains(view, "Owner:") {
		t.Fatal("missing task card boundaries or ownership")
	}
}

func TestMemberNavigationDoesNotPersistOutgoingSelection(t *testing.T) {
	var opened []string
	data := Snapshot{Active: true, Members: []Member{{ID: "a"}, {ID: "b"}}}
	models := []model{
		{kind: "members", current: "a", selectedID: "a", width: 24, height: 40, data: data},
		{kind: "members", current: "b", selectedID: "b", selected: 1, width: 24, height: 40, data: data},
	}
	for i := range models {
		models[i].act = func(a Action) error { opened = append(opened, a.Member); return nil }
	}
	for turn := 0; turn < 8; turn++ {
		source, target := turn%2, 1-turn%2
		m, cmd := models[source].Update(tea.MouseMsg{X: 4, Y: memberHeaderRows + target*memberBlockRows + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		models[source] = m.(model)
		if cmd == nil {
			t.Fatal("click did not navigate")
		}
		cmd()
		if opened[len(opened)-1] != models[target].current {
			t.Fatal("wrong destination")
		}
		if models[source].selectedID != models[source].current {
			t.Fatal("outgoing sidebar retained destination selection")
		}
		refreshed, _ := models[source].Update(snapshotMsg{data: data})
		models[source] = refreshed.(model)
		if models[source].selectedID != models[source].current {
			t.Fatal("refresh restored stale selection")
		}
	}
}

func TestDetailsButtonAndBack(t *testing.T) {
	m := model{kind: "tasks", width: 40, height: 50, data: Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Research", Detail: strings.Repeat("Evidence\n", 100), Milestones: []Milestone{{Name: "Research", State: "reported"}, {Name: "Review", State: "awaiting_approval", Gate: true}, {Name: "Deliver", State: "pending"}}}}}}
	_, hits := m.taskCards()
	next, cmd := m.Update(tea.MouseMsg{X: 4, Y: hits[0].button + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if cmd != nil || !m.detail {
		t.Fatal("details button did not open local detail view")
	}
	if !strings.Contains(ansi.Strip(m.View()), "1/3") || !strings.Contains(ansi.Strip(m.View()), "approval needed") {
		t.Fatal("milestone status lost")
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(model)
	if m.offset == 0 {
		t.Fatal("detail wheel did not scroll")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(model).detail || cmd != nil {
		t.Fatal("Escape closed panel instead of returning to tasks")
	}
}

func TestMemberNamesKeepTheirColumn(t *testing.T) {
	m := model{kind: "members", current: "a", width: 28, height: 40, data: Snapshot{Members: []Member{{ID: "a", Cwd: "~/project", Engine: "codex", State: "working", Tasks: "T1"}, {ID: "b", Cwd: "~/other"}}}}
	before := strings.Split(ansi.Strip(m.View()), "\n")
	m.current = "b"
	after := strings.Split(ansi.Strip(m.View()), "\n")
	row := memberHeaderRows + 1
	if strings.Index(before[row], "a") != strings.Index(after[row], "a") {
		t.Fatal("selection moved member name")
	}
	if !strings.Contains(strings.Join(after, "\n"), "Dir ~/project") {
		t.Fatal("working directory missing")
	}
}

func TestFooterButtonHitTargets(t *testing.T) {
	m := model{kind: "tasks", detail: true, width: 28, height: 40}
	b := m.footerButtons()[0]
	for _, x := range []int{b.start, b.end - 1} {
		next, cmd := m.Update(tea.MouseMsg{X: x, Y: m.height - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if cmd != nil || next.(model).detail {
			t.Fatal("Back button did not return to cards")
		}
	}
}

func TestMasterStaysPinnedAndSidebarCannotClose(t *testing.T) {
	m := model{kind: "members", current: "worker", width: 28, height: 40, data: Snapshot{Members: []Member{{ID: "master", Color: "115"}, {ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "worker"}}}}
	m.move(4)
	rows, hits := m.memberCards()
	if hits[0].index != 0 || hits[len(hits)-1].index != 4 || !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "◆ master") {
		t.Fatal("Master or selected worker scrolled out of view")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{'q'}}, {Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		_, cmd := m.Update(key)
		if cmd != nil {
			t.Fatal("sidebar still has a close shortcut")
		}
	}
	if strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "Close") {
		t.Fatal("sidebar still has a close button")
	}
}

func TestTaskHeaderCloseButton(t *testing.T) {
	var got Action
	m := model{kind: "tasks", width: 40, height: 40, act: func(a Action) error { got = a; return nil }}
	_, cmd := m.Update(tea.MouseMsg{X: 36, Y: taskTitleRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("header close button inactive")
	}
	cmd()
	if got.Kind != "close" {
		t.Fatalf("wrong action: %+v", got)
	}
}

func TestTaskFiltersKeepSelectionAndDetailsOnVisibleTasks(t *testing.T) {
	data := Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Historical task", State: "done"}, {ID: "T2", Title: "Current task", State: "in progress"}, {ID: "T3", Title: "Review task", State: "in review"}}}
	m := model{kind: "tasks", width: 40, height: 40, data: data}
	if m.count() != 2 || strings.Contains(ansi.Strip(m.View()), "Historical task") {
		t.Fatal("history leaked into active tasks")
	}
	next, _ := m.Update(tea.MouseMsg{X: 25, Y: taskFilterRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.count() != 1 || m.selectedID != "T1" {
		t.Fatal("Done filter selected wrong task")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(ansi.Strip(next.(model).View()), "Historical task") {
		t.Fatal("details opened the wrong filtered task")
	}
	m.filterTasks(false)
	m.selectedID = "T2"
	m.detail = true
	data.Tasks[1].State = "done"
	next, _ = m.Update(snapshotMsg{data: data})
	m = next.(model)
	if m.detail || m.count() != 1 || m.selectedID != "T3" {
		t.Fatal("completion left stale selection or details")
	}
	m.filterTasks(true)
	if m.count() != 2 {
		t.Fatal("completed task missing from history")
	}
}

// Tab must not leave the task list or discard an open task's details.
func TestTaskPanelHasNoAuxiliaryTabs(t *testing.T) {
	m := model{kind: "tasks", width: 40, height: 30, detail: true, data: Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Current work"}}}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || !next.(model).detail {
		t.Fatal("Tab changed task detail state")
	}
	for _, label := range []string{"Events", "Asks", "Tab Views"} {
		if strings.Contains(ansi.Strip(next.(model).View()), label) {
			t.Fatalf("obsolete navigation visible: %s", label)
		}
	}
}

func TestWheelScrollPreservesSelectionAcrossRefresh(t *testing.T) {
	for _, kind := range []string{"members", "tasks"} {
		t.Run(kind, func(t *testing.T) {
			data := Snapshot{Active: true, Members: []Member{{ID: "master"}, {ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}, Tasks: []Task{{ID: "T1", Title: "First"}, {ID: "T2", Title: "Second"}, {ID: "T3", Title: "Third"}}}
			m := model{kind: kind, current: "master", width: 40, height: 30, data: data}
			m.remember()
			selected := m.selectedID
			next, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			m = next.(model)
			if cmd != nil || m.selectedID != selected || m.selected != 0 {
				t.Fatal("wheel changed selection or dispatched navigation")
			}
			if kind == "members" && m.top <= 1 || kind == "tasks" && m.offset == 0 {
				t.Fatal("wheel did not scroll viewport")
			}
			top, offset := m.top, m.offset
			next, _ = m.Update(snapshotMsg{data: data})
			m = next.(model)
			if m.top != top || m.offset != offset || m.selectedID != selected {
				t.Fatal("refresh reset viewport or selection")
			}
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
			if next.(model).selected != 1 {
				t.Fatal("keyboard no longer changes selection")
			}
		})
	}
}

func TestWorkspaceHeaderIsBounded(t *testing.T) {
	m := model{kind: "header", width: 80, height: 3, current: "developer", data: Snapshot{Team: "example"}}
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if len(rows) != 3 || !strings.Contains(rows[1], "C SQUAD") || !strings.Contains(rows[1], "example") {
		t.Fatal("invalid workspace header", rows)
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 80 {
			t.Fatal("header is not full width")
		}
	}
}
