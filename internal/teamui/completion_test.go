package teamui

import (
	"fmt"
	"slices"
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

func TestTaskPanelGKeyDoesNotNavigate(t *testing.T) {
	for _, detail := range []bool{false, true} {
		m := taskModel(40, func(a Action) (string, error) {
			t.Fatalf("g dispatched action: %+v", a)
			return "", nil
		})
		m.detail = detail
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		if cmd != nil || next.(model).detail != detail {
			t.Fatalf("g changed task panel (detail=%v)", detail)
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

// The task panel has two tabs. Every unfinished phase, including legacy
// blocked and any unknown one, is under Active; done and cancelled share the
// second tab and keep distinct badges, and only done can carry the mark.
func TestTaskTabsSplitActiveFromDoneOrCancelled(t *testing.T) {
	active := []string{"ready", "preparing", "in progress", "in review", "awaiting merge", "merging", "blocked", "future phase"}
	m := model{kind: "tasks", width: 60, height: 200, data: Snapshot{Active: true}}
	for i, state := range active {
		m.data.Tasks = append(m.data.Tasks, Task{ID: fmt.Sprintf("A%d", i), Title: state, State: state})
	}
	m.data.Tasks = append(m.data.Tasks,
		Task{ID: "D1", Title: "shipped", State: "done", Completion: "✓ Completed · merged abc1234"},
		Task{ID: "C1", Title: "dropped", State: "cancelled", Note: "Cancelled · superseded"})
	ids := func() []string {
		var out []string
		for _, task := range m.tasks() {
			out = append(out, task.ID)
		}
		return out
	}
	if got := ids(); len(got) != len(active) || slices.Contains(got, "D1") || slices.Contains(got, "C1") {
		t.Fatalf("Active tab: %v", got)
	}
	filters := ansi.Strip(m.taskFilters())
	if !strings.Contains(filters, fmt.Sprintf("Active %d", len(active))) || !strings.Contains(filters, "Done 2") || strings.Contains(filters, "Cancelled") {
		t.Fatalf("tab labels: %q", filters)
	}
	m.filterTasks(true)
	if got := ids(); !slices.Equal(got, []string{"D1", "C1"}) {
		t.Fatalf("Done or cancelled tab: %v", got)
	}
	cards, _ := m.taskCards()
	text := ansi.Strip(strings.Join(cards, "\n"))
	for _, want := range []string{" Done ", " Cancelled ", "✓ Completed", "Cancelled · superseded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("finished cards lack %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "✓ Completed") != 1 {
		t.Fatalf("cancelled task shows the completion mark:\n%s", text)
	}
	narrow := model{kind: "tasks", width: 24, height: 40, data: m.data}
	if filters := ansi.Strip(narrow.taskFilters()); !strings.Contains(filters, "Done 2") || strings.Contains(filters, "…") {
		t.Fatalf("narrow tab label cut: %q", filters)
	}
}

func TestDetailsButtonFillsCardWithPaddedClickTarget(t *testing.T) {
	for _, width := range []int{12, 24, 40, 80} {
		rows, buttons := taskCardButtons(max(1, width-6))
		if len(rows) != 3 || len(buttons) != 1 || buttons[0].height != 3 || buttons[0].start != cardContentX || buttons[0].end != cardContentX+max(1, width-6) {
			t.Fatalf("width %d: button bounds: %+v", width, buttons)
		}
		for i, row := range rows {
			if got := ansi.StringWidth(row); got != max(1, width-6) {
				t.Fatalf("width %d: button row %d is %d columns", width, i, got)
			}
			if i != 1 && strings.TrimSpace(ansi.Strip(row)) != "" {
				t.Fatalf("width %d: vertical padding row %d has text: %q", width, i, row)
			}
		}
		if width >= 24 {
			plain := ansi.Strip(rows[1])
			if !strings.HasPrefix(plain, "  ") || !strings.HasSuffix(plain, "  ") {
				t.Fatalf("width %d: button lacks horizontal padding: %q", width, plain)
			}
		}
		for _, height := range []int{12, 16, 46} {
			m := taskModel(width, nil)
			m.height = height
			m.offset = m.maxOffset()
			_, hits := m.taskCards()
			if len(hits) == 0 || len(hits[0].buttons) == 0 {
				t.Fatalf("%dx%d: button missing from viewport", width, height)
			}
			button := hits[0].buttons[0]
			if button.height < 1 || button.height > 3 {
				t.Fatalf("%dx%d: invalid visible button height %+v", width, height, button)
			}
			for y := button.row; y < button.row+button.height; y++ {
				for _, x := range []int{button.start, button.end - 1} {
					next, _ := m.Update(tea.MouseMsg{X: x, Y: y + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
					if !next.(model).detail {
						t.Fatalf("%dx%d: painted button cell (%d,%d) did not open detail", width, height, x, y)
					}
				}
			}
			next, _ := m.Update(tea.MouseMsg{X: button.start - 1, Y: button.row + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if next.(model).detail {
				t.Fatalf("%dx%d: outer card gutter opened detail", width, height)
			}
		}
	}
}

func TestDetailBackPaddingHasMatchingClickBounds(t *testing.T) {
	for _, width := range []int{12, 28, 40} {
		rows, button := detailRow(width)
		if len(rows) != 3 || button.row != 2 || button.height != 3 || button.start != 2 || button.end != width-2 {
			t.Fatalf("width %d: back bounds %+v", width, button)
		}
		m := taskModel(width, nil)
		m.detail, m.height = true, 16
		for y := button.row; y < button.row+button.height; y++ {
			for _, x := range []int{button.start, button.end - 1} {
				next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				if next.(model).detail {
					t.Fatalf("width %d: painted back cell (%d,%d) did not return", width, x, y)
				}
			}
		}
	}
}

func TestScrolledButtonOnlyClicksPaintedRows(t *testing.T) {
	m := taskModel(40, nil)
	m.height = 12
	_, raw := m.taskCard(0)
	available := m.taskAvailable()
	m.offset = raw[0].row - available + 1
	_, hits := m.taskCards()
	if len(hits) != 1 || len(hits[0].buttons) != 1 || hits[0].buttons[0].height != 1 {
		t.Fatalf("partly clipped button has wrong visible bounds: %+v", hits)
	}
	button := hits[0].buttons[0]
	next, _ := m.Update(tea.MouseMsg{X: button.start, Y: button.row + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !next.(model).detail {
		t.Fatal("visible button padding did not open details")
	}
	next, _ = m.Update(tea.MouseMsg{X: button.start, Y: button.row + taskHeaderRows + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if next.(model).detail {
		t.Fatal("clipped button row remained clickable")
	}

	// A later card makes it possible to scroll the first button partly above
	// the viewport as well. Only its two remaining painted rows may activate it.
	m.data.Tasks = append(m.data.Tasks, Task{ID: "T2", Title: "Following task", State: "ready"})
	m.offset = raw[0].row + 1
	_, hits = m.taskCards()
	if len(hits) < 1 || hits[0].index != 0 || len(hits[0].buttons) != 1 || hits[0].buttons[0].row != 0 || hits[0].buttons[0].height != 2 {
		t.Fatalf("top-clipped button has wrong visible bounds: %+v", hits)
	}
	button = hits[0].buttons[0]
	for y := button.row; y < button.row+button.height; y++ {
		next, _ = m.Update(tea.MouseMsg{X: button.start, Y: y + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if !next.(model).detail {
			t.Fatalf("top-clipped painted row %d did not open details", y)
		}
	}
	next, _ = m.Update(tea.MouseMsg{X: button.start, Y: taskHeaderRows - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if next.(model).detail {
		t.Fatal("hidden top button row remained clickable")
	}
}
