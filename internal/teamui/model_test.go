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
	top := shown.top
	again, _ := shown.Update(snapshotMsg{data: Snapshot{Active: true, Members: members}})
	if again.(model).top != top {
		t.Fatal("a refresh snapped the list back to the selection")
	}
}

// An externally closed task must not render like a merged one.
func TestExternalClosureIsVisibleOnCardAndDetail(t *testing.T) {
	task := Task{ID: "T7", Title: "Cross repository work", State: "done", Owner: "dev",
		Note: "Closed externally · not merged · 9f3c1ab", Detail: "Closed externally by master — NOT merged"}
	m := model{kind: "tasks", width: 60, height: 40, completed: true, data: Snapshot{Active: true, Tasks: []Task{task}}}
	card := ansi.Strip(strings.Join(m.taskCard(0), "\n"))
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
	if strings.Contains(ansi.Strip(strings.Join(plain.taskCard(0), "\n")), "merged") {
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

func TestMemberScrollbarReflectsPosition(t *testing.T) {
	data := Snapshot{Active: true, Members: rosterOf(12)}
	m := model{kind: "members", width: 28, height: 34, current: "master", selectedID: "master", data: data}
	if m.rows() >= m.count()-1 {
		t.Fatalf("the roster fits in %d rows; this test needs it to overflow", m.rows())
	}
	positions := map[string][]int{}
	for _, at := range []struct {
		name string
		top  int
	}{{"top", 1}, {"middle", 5}, {"bottom", m.count() - m.rows()}} {
		scrolled := m
		scrolled.top = at.top
		var thumb []int
		for row, cell := range scrollColumn(scrolled) {
			if cell == '┃' {
				thumb = append(thumb, row)
			}
		}
		if len(thumb) == 0 {
			t.Fatalf("%s: no thumb drawn while %d of %d members are hidden", at.name, m.count()-1-m.rows(), m.count()-1)
		}
		positions[at.name] = thumb
	}
	if positions["top"][0] >= positions["middle"][0] || positions["middle"][0] >= positions["bottom"][0] {
		t.Fatalf("the thumb does not move down the track: %v", positions)
	}
	track := 0
	for _, cell := range scrollColumn(m) {
		if cell == '│' || cell == '┃' {
			track++
		}
	}
	bottom := positions["bottom"]
	if bottom[len(bottom)-1] != positions["top"][0]+track-1 {
		t.Fatalf("the bottom of the list does not reach the end of the track: %v in %d rows", positions, track)
	}
}

func TestShortRosterDrawsNoScrollbar(t *testing.T) {
	m := model{kind: "members", width: 28, height: 60, current: "master", data: Snapshot{Active: true, Members: rosterOf(2)}}
	if m.count()-1 > m.rows() {
		t.Fatal("this roster does not fit; the test cannot show the absence of a bar")
	}
	for _, cell := range scrollColumn(m) {
		if cell == '│' || cell == '┃' {
			t.Fatal("a fitting roster was given a bar implying hidden members")
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
		{name: "outside git keeps the divider", member: Member{ID: "dev", Cwd: "/tmp/plain"}, want: "────", unwanted: "Git "},
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

func TestScrollbarSurvivesResizeAndKeepsCardClicks(t *testing.T) {
	data := Snapshot{Active: true, Members: rosterOf(12)}
	m := model{kind: "members", width: 28, height: 34, current: "master", selectedID: "master", data: data}
	m.top = 1
	for _, size := range [][2]int{{28, 34}, {11, 34}, {24, 20}, {28, 34}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		resized := next.(model)
		for _, row := range strings.Split(resized.View(), "\n") {
			if ansi.StringWidth(row) > size[0] {
				t.Fatalf("width %d overflowed: %q", size[0], row)
			}
		}
		drawn := false
		for _, cell := range scrollColumn(resized) {
			drawn = drawn || cell == '│' || cell == '┃'
		}
		// Below the width block() itself gives up at, the bar is dropped rather
		// than squeezed into the card body.
		if want := size[0] >= 12; drawn != want {
			t.Fatalf("width %d drew the indicator=%v, want %v", size[0], drawn, want)
		}
	}
	// The bar occupies the gutter column only, and the card hit test is
	// unchanged, so a click there still opens that member.
	var opened Action
	m.act = func(a Action) error { opened = a; return nil }
	_, hits := m.memberCards()
	var scrolling cardHit
	for _, hit := range hits {
		if hit.index >= 1 {
			scrolling = hit
			break
		}
	}
	_, cmd := m.Update(tea.MouseMsg{X: m.width - 2, Y: scrolling.start + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("a click on the indicator column no longer reaches the member card")
	}
	cmd()
	if want := m.data.Members[scrolling.index].ID; opened.Kind != "open" || opened.Member != want {
		t.Fatalf("the indicator column opened %+v, want %s", opened, want)
	}
}
