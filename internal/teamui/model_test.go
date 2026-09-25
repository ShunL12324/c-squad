package teamui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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
		models[i].act = func(a Action) (string, error) { opened = append(opened, a.Member); return "", nil }
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
	next, cmd := m.Update(tea.MouseMsg{X: 4, Y: hits[0].buttons[0].row + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if cmd != nil || !m.detail {
		t.Fatal("details button did not open local detail view")
	}
	if !strings.Contains(ansi.Strip(m.View()), "1/3") || !strings.Contains(ansi.Strip(m.View()), "approval needed") {
		t.Fatal("milestone status lost")
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(model)
	if m.viewport.YOffset == 0 {
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

func TestTaskDetailsUseHeaderBackWithoutFooter(t *testing.T) {
	m := model{kind: "tasks", detail: true, width: 28, height: 40, data: Snapshot{Tasks: []Task{{ID: "T1", Title: "Research"}}}}
	if footer := m.footer(); len(footer) != 0 {
		t.Fatalf("task detail still has a footer: %q", footer)
	}
	next, cmd := m.Update(tea.MouseMsg{X: 3, Y: taskFilterRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil || next.(model).detail {
		t.Fatal("header Back button did not return to cards")
	}
	list := next.(model)
	if footer := list.footer(); len(footer) != 0 {
		t.Fatalf("task list still has a footer: %q", footer)
	}
}

func TestMasterStaysPinnedAndSidebarCannotClose(t *testing.T) {
	m := model{kind: "members", current: "worker", width: 28, height: 40, data: Snapshot{Members: []Member{{ID: "master", Color: "115"}, {ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "worker"}}}}
	for range 4 {
		m.move(1)
	}
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
	m := model{kind: "tasks", width: 40, height: 40, act: func(a Action) (string, error) { got = a; return "", nil }}
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
			if kind == "members" && m.pages.Page == 0 || kind == "tasks" && m.viewport.YOffset == 0 {
				t.Fatal("wheel did not scroll viewport")
			}
			top, offset := m.pages.Page, m.viewport.YOffset
			next, _ = m.Update(snapshotMsg{data: data})
			m = next.(model)
			if m.pages.Page != top || m.viewport.YOffset != offset || m.selectedID != selected {
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

// cellBackgrounds maps each rendered column to the 256-colour background in
// effect there, so a test can prove a painted row has no unstyled holes.
func cellBackgrounds(row string) []string {
	var cells []string
	bg := ""
	for i := 0; i < len(row); {
		if strings.HasPrefix(row[i:], "\x1b[") {
			end := strings.IndexByte(row[i:], 'm')
			if end < 0 {
				break
			}
			params := strings.Split(row[i+2:i+end], ";")
			for j := 0; j < len(params); j++ {
				switch params[j] {
				case "", "0":
					bg = ""
				case "38", "48":
					if j+2 < len(params) && params[j+1] == "5" {
						if params[j] == "48" {
							bg = params[j+2]
						}
						j += 2
					}
				}
			}
			i += end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(row[i:])
		for range ansi.StringWidth(string(r)) {
			cells = append(cells, bg)
		}
		i += size
	}
	return cells
}

func TestTaskFilterSegmentsSplitPanelEvenly(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	data := Snapshot{Active: true, Tasks: []Task{{ID: "T1", State: "done"}, {ID: "T2"}, {ID: "T3"}}}
	for _, width := range []int{24, 30, 40, 41} {
		for _, completed := range []bool{false, true} {
			m := model{kind: "tasks", width: width, height: 20, completed: completed, data: data}
			left, right := m.filterSplit()
			if left+right != width-4 || max(left, right)-min(left, right) > 1 {
				t.Fatalf("segments do not split %d evenly: %d/%d", width-4, left, right)
			}
			row := strings.Split(m.View(), "\n")[taskFilterRow]
			cells := cellBackgrounds(row)
			if len(cells) != width {
				t.Fatalf("filter row is %d columns wide, want %d", len(cells), width)
			}
			leftBG, rightBG := accent, surface
			if completed {
				leftBG, rightBG = surface, accent
			}
			want := func(x int) string {
				switch {
				case x < 2 || x >= width-2:
					return canvas
				case x < 2+left:
					return leftBG
				default:
					return rightBG
				}
			}
			for x, bg := range cells {
				if bg != want(x) {
					t.Fatalf("width %d completed %v: column %d painted %q, want %q in %q",
						width, completed, x, bg, want(x), ansi.Strip(row))
				}
			}
		}
	}
}

func TestTaskFilterClicksFollowRenderedSegments(t *testing.T) {
	data := Snapshot{Active: true, Tasks: []Task{{ID: "T1", State: "done"}, {ID: "T2"}}}
	click := func(m model, x int) bool {
		next, _ := m.Update(tea.MouseMsg{X: x, Y: taskFilterRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		return next.(model).completed
	}
	for _, width := range []int{1, 12, 24, 30, 40, 41} {
		active := model{kind: "tasks", width: width, height: 20, data: data}
		done := active
		done.completed = true
		left, _ := active.filterSplit()
		// Gutters belong to the segment beside them, so every column of the
		// row selects one filter and none is dead.
		for x := range width {
			if got, want := click(active, x), x >= 2+left; got != want {
				t.Fatalf("width %d: click at %d gave completed=%v, want %v", width, x, got, want)
			}
			if got, want := click(done, x), x >= 2+left; got != want {
				t.Fatalf("width %d: click at %d from Done gave completed=%v, want %v", width, x, got, want)
			}
		}
	}
	wide := model{kind: "tasks", width: 40, height: 20, completed: true, data: data}
	if click(wide, 10) {
		t.Fatal("left half must select Active")
	}
	if !click(model{kind: "tasks", width: 40, height: 20, data: data}, 30) {
		t.Fatal("right half must select Done")
	}
}

func TestPanelShowsLoadingUntilTheFirstRead(t *testing.T) {
	m := model{kind: "members", width: 28, height: 20, loading: true}
	if !strings.Contains(ansi.Strip(m.View()), "Loading") {
		t.Fatal("panel rendered an empty region before its first snapshot")
	}
	data := Snapshot{Active: true, Switch: "Alt+↑↓", Members: []Member{{ID: "master"}, {ID: "dev"}}}
	next, _ := m.Update(snapshotMsg{data: data})
	view := ansi.Strip(next.(model).View())
	if strings.Contains(view, "Loading") || !strings.Contains(view, "dev") {
		t.Fatal("loading state survived the first snapshot:", view)
	}
	if !strings.Contains(view, "Alt+↑↓ Switch") {
		t.Fatal("member footer does not name the switch keys:", view)
	}
	bare, _ := model{kind: "members", width: 28, height: 20, loading: true}.Update(snapshotMsg{data: Snapshot{Active: true, Members: data.Members}})
	if !strings.Contains(ansi.Strip(bare.(model).View()), "↑↓ Select") {
		t.Fatal("unbound switch keys should leave the original hint in place")
	}
}

func TestSidebarRevealsTheCurrentMemberOnFirstRead(t *testing.T) {
	var members []Member
	for i := range 20 {
		members = append(members, Member{ID: fmt.Sprintf("member-%02d", i)})
	}
	m := model{kind: "members", width: 28, height: 24, loading: true, current: "member-17", selectedID: "member-17"}
	next, _ := m.Update(snapshotMsg{data: Snapshot{Active: true, Members: members}})
	shown := next.(model)
	if !strings.Contains(ansi.Strip(shown.View()), "member-17") {
		t.Fatal("a long roster left the current member scrolled out of view")
	}
	// Later reads must not fight the wheel.
	shown.scroll(-1)
	top := shown.pages.Page
	again, _ := shown.Update(snapshotMsg{data: Snapshot{Active: true, Members: members}})
	if again.(model).pages.Page != top {
		t.Fatal("a refresh snapped the list back to the selection")
	}
}

// An externally closed task must not render like a merged one.
func TestExternalClosureIsVisibleOnCardAndDetail(t *testing.T) {
	task := Task{ID: "T7", Title: "Cross repository work", State: "done", Owner: "dev",
		Note: "Closed externally · not merged · 9f3c1ab", Detail: "Closed externally by master — NOT merged"}
	m := model{kind: "tasks", width: 60, height: 40, completed: true, data: Snapshot{Active: true, Tasks: []Task{task}}}
	card := ansi.Strip(strings.Join(cardLines(m, 0), "\n"))
	if !strings.Contains(card, "not merged") {
		t.Errorf("card does not mark the external closure: %q", card)
	}
	m.detail = true
	detail := ansi.Strip(strings.Join(m.boardView(), "\n"))
	for _, want := range []string{"not merged", "NOT merged"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail view %q does not mention %q", detail, want)
		}
	}
	plain := model{kind: "tasks", width: 60, height: 40, completed: true,
		data: Snapshot{Active: true, Tasks: []Task{{ID: "T8", Title: "Merged work", State: "done", Owner: "dev"}}}}
	if strings.Contains(ansi.Strip(strings.Join(cardLines(plain, 0), "\n")), "merged") {
		t.Error("a merged task card gained an external closure marker")
	}
}

// scrollColumn reports the indicator cell of every rendered row, which lives in
// the gutter column the cards leave blank.
func scrollColumn(m model) []rune {
	var cells []rune
	for _, row := range strings.Split(ansi.Strip(m.View()), "\n") {
		runes := []rune(row)
		if len(runes) < m.width-1 {
			cells = append(cells, ' ')
			continue
		}
		cells = append(cells, runes[m.width-2])
	}
	return cells
}

func rosterOf(n int) []Member {
	members := []Member{{ID: "master", Engine: "claude", State: "idle", Cwd: "/repo", Branch: "main"}}
	for i := 1; i < n; i++ {
		members = append(members, Member{ID: fmt.Sprintf("member-%02d", i), Engine: "codex", State: "idle", Cwd: "/repo", Branch: "main"})
	}
	return members
}

func TestMemberPaginationReflectsPosition(t *testing.T) {
	m := model{kind: "members", width: 28, height: 34, data: Snapshot{Active: true, Members: rosterOf(12)}}
	l := m.memberList()
	if l.Paginator.TotalPages < 2 {
		t.Fatal("roster needs pagination")
	}
	first := ansi.Strip(m.View())
	m.scroll(1)
	if m.pages.Page != 1 || ansi.Strip(m.View()) == first {
		t.Fatal("stock paginator did not advance")
	}
	for range 20 {
		m.scroll(1)
	}
	if m.pages.Page != l.Paginator.TotalPages-1 {
		t.Fatal("page did not stop at end")
	}
	m.scroll(-1)
	if m.pages.Page != l.Paginator.TotalPages-2 {
		t.Fatal("reverse after end failed")
	}
}

func TestShortRosterHasOnePage(t *testing.T) {
	m := model{kind: "members", width: 28, height: 60, data: Snapshot{Active: true, Members: rosterOf(2)}}
	if m.memberList().Paginator.TotalPages != 1 {
		t.Fatal("fitting roster has extra pages")
	}
}

func TestMemberResizeFillsViewportAfterInitialSnapshot(t *testing.T) {
	// A detached panel may read its roster before receiving its real size.
	// At the initial height only the owner fits below the pinned Master.
	m := model{kind: "members", width: 24, height: 24, current: "b", selectedID: "b", loading: true}
	data := Snapshot{Active: true, Members: []Member{{ID: "master"}, {ID: "a"}, {ID: "b", Color: "115"}}}
	next, _ := m.Update(snapshotMsg{data: data})
	m = next.(model)
	if m.pages.Page != 1 {
		t.Fatalf("initial viewport should reveal b, got top %d", m.pages.Page)
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 28, Height: 34})
	m = next.(model)
	if m.pages.Page != 0 {
		t.Fatalf("expanded viewport still hides a: top=%d\n%s", m.pages.Page, ansi.Strip(m.View()))
	}
	for _, cell := range scrollColumn(m) {
		if cell == '│' || cell == '┃' {
			t.Fatal("expanded roster fits but still shows a scrollbar")
		}
	}
}

func TestMemberCardShowsItsOwnGitState(t *testing.T) {
	for _, tt := range []struct {
		name           string
		member         Member
		want, unwanted string
	}{
		{name: "plain branch", member: Member{ID: "dev", Cwd: "/repo", Branch: "main"}, want: "Git main"},
		{name: "detached head", member: Member{ID: "dev", Cwd: "/repo", Commit: "4f2a1b9"}, want: "Git detached 4f2a1b9"},
		{name: "linked worktree keeps the branch tail and the marker",
			member: Member{ID: "dev", Cwd: "/repo", Branch: "csquad/csquad/T167", Worktree: true}, want: "wt", unwanted: "Git csquad/csquad/T167"},
		{name: "outside git keeps the divider", member: Member{ID: "dev", Cwd: "/tmp/plain"}, want: "—", unwanted: "Git "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{kind: "members", width: 28, height: 40, data: Snapshot{Active: true, Members: []Member{tt.member}}}
			card := ansi.Strip(strings.Join(m.memberCard(0), "\n"))
			if !strings.Contains(card, tt.want) {
				t.Fatalf("card is missing %q:\n%s", tt.want, card)
			}
			if tt.unwanted != "" && strings.Contains(card, tt.unwanted) {
				t.Fatalf("card unexpectedly contains %q:\n%s", tt.unwanted, card)
			}
			if len(m.memberCard(0)) != memberBlockRows {
				t.Fatalf("card is %d rows, want %d: the Git line must reuse the divider's row", len(m.memberCard(0)), memberBlockRows)
			}
		})
	}
	// The tail identifies the branch; the head is the part that repeats.
	wide := model{kind: "members", width: 28, height: 40, data: Snapshot{Active: true,
		Members: []Member{{ID: "dev", Cwd: "/repo", Branch: "csquad/csquad/T167", Worktree: true}}}}
	if card := ansi.Strip(strings.Join(wide.memberCard(0), "\n")); !strings.Contains(card, "T167") {
		t.Fatalf("truncation dropped the identifying end of the branch:\n%s", card)
	}
}

func TestMemberPaginationResizeAndClicks(t *testing.T) {
	data := Snapshot{Active: true, Members: rosterOf(12)}
	m := model{kind: "members", width: 28, height: 34, current: "master", selectedID: "master", data: data}
	m.scroll(1)
	for _, size := range [][2]int{{28, 34}, {11, 34}, {24, 20}, {28, 34}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(model)
		for _, row := range strings.Split(m.View(), "\n") {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("resized list overflow")
			}
		}
	}
	var opened Action
	m.act = func(a Action) (string, error) { opened = a; return "", nil }
	_, hits := m.memberCards()
	for _, hit := range hits {
		if hit.index == 0 {
			continue
		}
		_, cmd := m.Update(tea.MouseMsg{X: 4, Y: hit.start + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if cmd == nil {
			t.Fatal("visible list item not clickable")
		}
		cmd()
		if opened.Member != m.data.Members[hit.index].ID {
			t.Fatal("wrong item after pagination/resize")
		}
		return
	}
	t.Fatal("no scrolling list items")
}

// cardLines renders one card without its button rectangles.
func cardLines(m model, index int) []string {
	card, _ := m.taskCard(index)
	return card
}

// Scrolling past the end of a task's details stops at the last page, so the
// first press back up moves the view again (#29). Keys, page keys and the wheel
// all share the bound.
func TestDetailScrollStopsAtTheEnd(t *testing.T) {
	m := model{kind: "tasks", width: 40, height: 30, detail: true, data: Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Research", Detail: strings.Repeat("Evidence\n", 60)}}}}
	limit := m.maxOffset()
	if limit == 0 {
		t.Fatal("fixture does not overflow the panel")
	}
	press := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	for range limit + 20 {
		press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if m.viewport.YOffset != limit {
		t.Fatalf("j scrolled to %d, past the last page at %d", m.viewport.YOffset, limit)
	}
	last := ansi.Strip(m.View())
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.viewport.YOffset != limit-1 || ansi.Strip(m.View()) == last {
		t.Fatalf("one k after overscrolling did not move the view: offset %d", m.viewport.YOffset)
	}
	for range 10 {
		press(tea.KeyMsg{Type: tea.KeyPgDown})
		press(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	if m.viewport.YOffset != limit {
		t.Fatalf("page and wheel scrolled to %d, past %d", m.viewport.YOffset, limit)
	}
}

// The last page also moves when the pane grows or the details shrink. The stored
// offset follows, so the first k after either still moves the view.
func TestDetailScrollBoundFollowsResizeAndContent(t *testing.T) {
	task := Task{ID: "T1", Title: "Research", Detail: strings.Repeat("Evidence\n", 60)}
	m := model{kind: "tasks", width: 40, height: 30, detail: true, selectedID: "T1", data: Snapshot{Active: true, Tasks: []Task{task}}}
	update := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	firstKMoves := func(when string) {
		t.Helper()
		if m.viewport.YOffset != m.maxOffset() {
			t.Fatalf("%s: offset %d, last page at %d", when, m.viewport.YOffset, m.maxOffset())
		}
		before := ansi.Strip(m.View())
		update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		if ansi.Strip(m.View()) == before {
			t.Fatalf("%s: the first k did not move the view", when)
		}
	}
	for range 100 {
		update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	update(tea.WindowSizeMsg{Width: 40, Height: 50})
	firstKMoves("after enlarging the pane")

	for range 100 {
		update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	task.Detail = strings.Repeat("Evidence\n", 40)
	update(snapshotMsg{data: Snapshot{Active: true, Tasks: []Task{task}}})
	firstKMoves("after the details shrank")
}

func TestCurrentMemberCueSurvivesCursorMovement(t *testing.T) {
	m := model{kind: "members", current: "master", selectedID: "master", width: 40, height: 40, data: Snapshot{Active: true, Members: []Member{{ID: "master", Engine: "codex"}, {ID: "dev", Engine: "codex"}}}}
	m.move(1)
	if m.selectedID != "dev" {
		t.Fatal("standard list did not move cursor")
	}
	current := ansi.Strip(strings.Join(m.memberCard(0), "\n"))
	other := ansi.Strip(strings.Join(m.memberCard(1), "\n"))
	if !strings.Contains(current, "● Codex") || strings.Contains(other, "● Codex") {
		t.Fatal("current-session cue followed cursor")
	}
	for range 8 {
		m.move(1)
	}
	if m.selectedID != "dev" {
		t.Fatal("list cursor wrapped past end")
	}
	m.move(-1)
	if m.selectedID != "master" {
		t.Fatal("cannot return to pinned Master")
	}
}
