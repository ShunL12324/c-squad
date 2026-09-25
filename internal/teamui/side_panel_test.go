package teamui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func panelForTest(kind, current string, width, height int, data Snapshot) *sidePanel {
	p := &sidePanel{app: tview.NewApplication(), root: tview.NewFlex().SetDirection(tview.FlexRow),
		kind: kind, current: current, data: data, width: width, height: height, selectedID: current}
	p.reconcile()
	if kind == "members" {
		p.reveal(p.memberIndex(p.selectedID))
	}
	p.render()
	return p
}

func TestSidePanelUsesNativeButtonsAndPreservesTaskDetail(t *testing.T) {
	p := panelForTest("tasks", "master", 40, 46, Snapshot{Active: true, Tasks: []Task{
		{ID: "T1", Title: "[red] literal", State: "blocked", Owner: "dev", Progress: "[blue] update", Detail: "[green] details", Milestones: []Milestone{{Name: "[yellow] Review", State: "awaiting_approval", Gate: true}}},
		{ID: "T2", Title: "Cancelled", State: "cancelled"},
	}})
	button := p.taskButtons["T1"]
	if button == nil || button.IsDisabled() {
		t.Fatal("blocked work must still have an enabled tview Button")
	}
	button.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), p.app.SetFocus)
	if p.detailID != "T1" || p.detailText == nil {
		t.Fatal("native Enter did not open task details")
	}
	text := p.detailText.GetText(false)
	for _, want := range []string{"MILESTONES  0/1", "[yellow[] Review", "[green[] details", "[blue[] update"} {
		if !strings.Contains(text, want) {
			t.Fatalf("details lost %q: %s", want, text)
		}
	}
	p.detailID = ""
	p.filter(true)
	if p.taskButtons["T2"] == nil || p.taskButtons["T1"] != nil {
		t.Fatal("Done tab did not retain cancelled task")
	}
	button = p.taskButtons["T2"]
	button.SetRect(0, 0, 38, 3)
	button.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(5, 1, tcell.ButtonPrimary, 0), p.app.SetFocus)
	if p.detailID != "T2" {
		t.Fatal("native Button click did not open cancelled task details")
	}
}

func TestMemberCardsPinMasterAndPageEveryWorker(t *testing.T) {
	members := []Member{{ID: "master", State: "working", Tasks: "T1"}}
	for i := 0; i < 12; i++ {
		members = append(members, Member{ID: "worker-" + string(rune('a'+i)), State: "idle"})
	}
	p := panelForTest("members", "worker-l", 32, 46, Snapshot{Active: true, Members: members})
	if p.memberCards["master"] == nil {
		t.Fatal("Master is not pinned")
	}
	if p.memberCards["worker-l"] == nil || p.memberCards["master"] == nil {
		t.Fatal("initial page failed to reveal current worker with Master pinned")
	}
	lastPage := p.page
	p.mouse(tcell.NewEventMouse(0, 0, 0, 0), tview.MouseScrollUp)
	if p.page >= lastPage || p.selectedID != "worker-l" {
		t.Fatal("wheel page must be independent of selection")
	}
}

func TestMemberCurrentAndFocusHaveDistinctCardStyles(t *testing.T) {
	p := panelForTest("members", "master", 32, 46, Snapshot{Active: true, Members: []Member{{ID: "master", State: "working", Tasks: "T477, T480"}, {ID: "dev", State: "blocked", Tasks: "T451"}}})
	p.selectedID = "dev"
	p.render()
	current, focus := p.memberCards["master"], p.memberCards["dev"]
	if current == nil || focus == nil {
		t.Fatal("both cards must render")
	}
	if current.GetBackgroundColor() != uiCurrent || focus.GetBackgroundColor() != uiCard {
		t.Fatal("current and focus surfaces collapsed")
	}
	if current.GetBorderColor() != uiBlue || focus.GetBorderColor() != uiAccent {
		t.Fatal("current border and focus border collapsed")
	}
	if strings.Contains(current.GetText(false), "●") {
		t.Fatal("obsolete current-session dot returned")
	}
	if !strings.Contains(current.GetText(false), "T477") || !strings.Contains(current.GetText(false), "T480") {
		t.Fatal("multiple task chips lost")
	}
}

func TestButtonTreatsSecondPhysicalClickAsActivation(t *testing.T) {
	count := 0
	button := styledButton("View details", false, func() { count++ })
	button.SetRect(0, 0, 30, 3)
	setFocus := func(tview.Primitive) {}
	for _, action := range []tview.MouseAction{tview.MouseLeftClick, tview.MouseLeftDoubleClick} {
		button.MouseHandler()(action, tcell.NewEventMouse(5, 1, tcell.ButtonPrimary, 0), setFocus)
	}
	if count != 2 {
		t.Fatalf("two physical clicks activated %d times", count)
	}
}

func TestMemberDataCannotInjectTviewColorTags(t *testing.T) {
	id := "[red] dev"
	p := panelForTest("members", id, 32, 24, Snapshot{Active: true, Members: []Member{{ID: id, Cwd: "[blue] /tmp", Tasks: "[yellow] T1"}}})
	text := p.memberCards[id].GetText(false)
	for _, want := range []string{"[red[] dev", "[blue[] /tmp", "[yellow[] T1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("unescaped member text %q: %s", want, text)
		}
	}
}

func drawPanel(t *testing.T, p *sidePanel) []string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(p.width, p.height)
	p.root.SetRect(0, 0, p.width, p.height)
	p.root.Draw(screen)
	var lines []string
	for y := 0; y < p.height; y++ {
		var row strings.Builder
		for x := 0; x < p.width; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			if r == 0 {
				r = ' '
			}
			row.WriteRune(r)
		}
		lines = append(lines, row.String())
	}
	return lines
}

func TestShortPanesKeepWorkerAndNativeTaskButtonVisible(t *testing.T) {
	for _, height := range []int{12, 16, 20} {
		members := panelForTest("members", "worker", 28, 46, Snapshot{Active: true, Members: []Member{{ID: "master", State: "idle"}, {ID: "other", State: "blocked"}, {ID: "worker", State: "working", Tasks: "T477"}}})
		members.resize(28, height)
		memberLines := drawPanel(t, members)
		if card := members.memberCards["worker"]; card == nil {
			t.Fatalf("height %d: selected worker missing", height)
		} else if _, y, _, h := card.GetRect(); y+h > height {
			t.Fatalf("height %d: worker card clipped at %d", height, y+h)
		}
		if !strings.Contains(strings.Join(memberLines, "\n"), "worker") {
			t.Fatalf("height %d: worker title invisible", height)
		}
		for _, width := range []int{28, 40} {
			tasks := panelForTest("tasks", "master", width, 46, Snapshot{Active: true, Tasks: []Task{{ID: "T477", Title: "A long task title wraps at narrow widths", State: "review", Owner: "worker"}}})
			tasks.resize(width, height)
			lines := drawPanel(t, tasks)
			button := tasks.taskButtons["T477"]
			if button == nil || button.IsDisabled() {
				t.Fatalf("%dx%d: task button unavailable", width, height)
			}
			if _, y, _, h := button.GetRect(); y+h > height {
				t.Fatalf("%dx%d: task button clipped at %d", width, height, y+h)
			}
			if !strings.Contains(strings.Join(lines, "\n"), "View details") {
				t.Fatalf("%dx%d: button label invisible", width, height)
			}
		}
	}
}

func TestPollingErrorRendersPlainTextInShortPane(t *testing.T) {
	p := panelForTest("tasks", "master", 28, 12, Snapshot{Active: true, Tasks: []Task{{ID: "T1", State: "working"}}})
	p.updateSnapshot(Snapshot{}, errors.New("[red] ledger unavailable"))
	view := strings.Join(drawPanel(t, p), "\n")
	if !strings.Contains(view, "[red] ledger") || strings.Contains(view, "[red[]") {
		t.Fatalf("error did not render as plain text: %s", view)
	}
}

func TestShortTaskWheelReachesLastAction(t *testing.T) {
	p := panelForTest("tasks", "master", 40, 12, Snapshot{Active: true, Tasks: []Task{{ID: "T1"}, {ID: "T2"}, {ID: "T3"}}})
	for range 2 {
		p.mouse(tcell.NewEventMouse(0, 0, 0, 0), tview.MouseScrollDown)
	}
	if p.page != 2 || p.taskButtons["T3"] == nil {
		t.Fatalf("last task unreachable: page %d", p.page)
	}
	lines := drawPanel(t, p)
	if !strings.Contains(strings.Join(lines, "\n"), "View details") {
		t.Fatal("last task action clipped")
	}
}

func TestMemberTabThenEnterOpensFocusedCard(t *testing.T) {
	p := panelForTest("members", "master", 28, 20, Snapshot{Active: true, Members: []Member{{ID: "master"}, {ID: "worker"}}})
	opened := make(chan Action, 1)
	p.act = func(a Action) (string, error) { opened <- a; return "opened", nil }
	screen := tcell.NewSimulationScreen("UTF-8")
	p.app.SetScreen(screen)
	screen.SetSize(28, 20)
	done := make(chan error, 1)
	go func() { done <- p.app.Run() }()
	defer func() { p.app.Stop(); <-done }()
	p.app.QueueUpdateDraw(func() {
		p.key(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		if p.selectedID != "worker" {
			t.Errorf("Tab did not select focused worker: %q", p.selectedID)
		}
		p.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	})
	select {
	case a := <-opened:
		if a.Kind != "open" || a.Member != "worker" {
			t.Fatalf("Enter opened wrong member: %+v", a)
		}
	case <-time.After(time.Second):
		t.Fatal("Enter did not dispatch member action")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		seen := false
		p.app.QueueUpdate(func() { seen = p.errorText == "opened" })
		if seen {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("member action callback did not complete")
}

func TestRootMouseClickOpensOnlyLaterMember(t *testing.T) {
	p := panelForTest("members", "master", 32, 46, Snapshot{Active: true, Members: []Member{
		{ID: "master"}, {ID: "first"}, {ID: "second"},
	}})
	opened := make(chan Action, 2)
	p.act = func(a Action) (string, error) { opened <- a; return "opened", nil }
	screen := tcell.NewSimulationScreen("UTF-8")
	p.app.SetScreen(screen)
	screen.SetSize(32, 46)
	done := make(chan error, 1)
	go func() { done <- p.app.Run() }()
	defer func() { p.app.Stop(); <-done }()
	p.app.QueueUpdateDraw(func() {
		p.root.SetRect(0, 0, 32, 46)
		p.root.Draw(screen)
		card := p.memberCards["second"]
		if card == nil {
			t.Error("later member card is not visible")
			return
		}
		x, y, _, _ := card.GetRect()
		consumed, _ := p.root.MouseHandler()(tview.MouseLeftClick,
			tcell.NewEventMouse(x+2, y+1, tcell.ButtonPrimary, 0), p.app.SetFocus)
		if !consumed {
			t.Error("member click propagated after card rebuild")
		}
	})
	select {
	case a := <-opened:
		if a.Kind != "open" || a.Member != "second" {
			t.Fatalf("later card click opened wrong member: %+v", a)
		}
	case <-time.After(time.Second):
		t.Fatal("later card click did not open a member")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		seen := false
		p.app.QueueUpdate(func() { seen = p.errorText == "opened" })
		if seen {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("member click callback did not complete")
}

func TestRootMouseClickSelectsOnlyLaterTaskAndButton(t *testing.T) {
	p := panelForTest("tasks", "master", 40, 46, Snapshot{Active: true, Tasks: []Task{
		{ID: "T1", Title: "First"}, {ID: "T2", Title: "Second"},
	}})
	drawPanel(t, p)
	var body *tview.TextView
	for i := 0; i < p.root.GetItemCount(); i++ {
		card, ok := p.root.GetItem(i).(*tview.Flex)
		if !ok || card.GetItemCount() != 2 || card.GetItem(1) != p.taskButtons["T2"] {
			continue
		}
		body, _ = card.GetItem(0).(*tview.TextView)
		break
	}
	if body == nil {
		t.Fatal("later task body is not visible")
	}
	x, y, _, _ := body.GetRect()
	consumed, _ := p.root.MouseHandler()(tview.MouseLeftClick,
		tcell.NewEventMouse(x+2, y+1, tcell.ButtonPrimary, 0), p.app.SetFocus)
	if !consumed || p.selectedID != "T2" || p.detailID != "" {
		t.Fatalf("later task body click propagated or selected another task: consumed=%t selected=%q detail=%q", consumed, p.selectedID, p.detailID)
	}
	drawPanel(t, p)
	button := p.taskButtons["T2"]
	x, y, _, _ = button.GetRect()
	consumed, _ = p.root.MouseHandler()(tview.MouseLeftClick,
		tcell.NewEventMouse(x+2, y+1, tcell.ButtonPrimary, 0), p.app.SetFocus)
	if !consumed || p.selectedID != "T2" || p.detailID != "T2" {
		t.Fatalf("later task button click activated another card: consumed=%t selected=%q detail=%q", consumed, p.selectedID, p.detailID)
	}
}
