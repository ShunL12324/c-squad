package teamui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func panelForTest(kind, current string, width, height int, data Snapshot) *sidePanel {
	p := &sidePanel{app: tview.NewApplication(), root: tview.NewFlex().SetDirection(tview.FlexRow),
		kind: kind, current: current, data: data, width: width, height: height, selectedID: current}
	p.reconcile()
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
	if p.memberCards["worker-l"] != nil {
		t.Fatal("initial page unexpectedly includes last worker")
	}
	p.move(1)
	if p.memberCards["worker-l"] == nil || p.memberCards["master"] == nil {
		t.Fatal("keyboard failed to reveal last worker with Master pinned")
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
