package teamui

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The side panels use tview's controls for layout, focus and activation. The
// snapshot and action callbacks remain the only connection to team state.
type sidePanel struct {
	app           *tview.Application
	root          *tview.Flex
	kind, current string
	load          Source
	act           Handler
	data          Snapshot
	width, height int
	selectedID    string
	completed     bool
	detailID      string
	page          int
	userPaged     bool
	errorText     string
	memberCards   map[string]*tview.TextView
	taskButtons   map[string]*tview.Button
	tabs          [2]*tview.Button
	detailText    *tview.TextView
	focusOrder    []tview.Primitive
}

var (
	uiCanvas  = tcell.PaletteColor(234)
	uiCard    = tcell.PaletteColor(236)
	uiCurrent = tcell.PaletteColor(238)
	uiText    = tcell.PaletteColor(253)
	uiMuted   = tcell.PaletteColor(245)
	uiAccent  = tcell.PaletteColor(115)
	uiBlue    = tcell.PaletteColor(81)
	uiWarning = tcell.PaletteColor(222)
)

func runSidePanel(kind, current string, load Source, act Handler) error {
	p := &sidePanel{app: tview.NewApplication(), root: tview.NewFlex().SetDirection(tview.FlexRow),
		kind: kind, current: current, load: load, act: act, selectedID: current, width: 28, height: 46}
	p.app.EnableMouse(true)
	p.app.SetInputCapture(p.key)
	p.app.SetMouseCapture(p.mouse)
	// A resize is drawn by tview first. Queue the layout update after that draw;
	// SetRoot/SetFocus must never run inside tview's locked draw callback.
	p.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		width, height := screen.Size()
		if width != p.width || height != p.height {
			go p.app.QueueUpdateDraw(func() { p.resize(width, height) })
		}
	})
	data, err := load()
	if err == nil {
		if !data.Active {
			return nil
		}
		p.data = data
	} else {
		p.errorText = err.Error()
	}
	p.reconcile()
	if p.kind == "members" {
		p.reveal(p.memberIndex(p.selectedID))
	}
	p.render()
	stopped := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ticker.C:
				data, err := load()
				p.app.QueueUpdateDraw(func() { p.updateSnapshot(data, err) })
			}
		}
	}()
	err = p.app.Run()
	close(stopped)
	return err
}

func (p *sidePanel) resize(width, height int) {
	p.width, p.height = width, height
	if p.userPaged {
		p.clampPage()
	} else if p.kind == "members" {
		p.reveal(p.memberIndex(p.selectedID))
	} else {
		p.reveal(p.taskIndex(p.selectedID))
	}
	p.render()
}

func (p *sidePanel) updateSnapshot(data Snapshot, err error) {
	if err != nil {
		if p.errorText != err.Error() {
			p.errorText = err.Error()
			p.render()
		}
		return
	}
	if !data.Active {
		p.app.Stop()
		return
	}
	if !reflect.DeepEqual(data, p.data) || p.errorText != "" {
		p.data, p.errorText = data, ""
		p.reconcile()
		p.render()
	}
}

func (p *sidePanel) reconcile() {
	prior := p.selectedID
	if p.kind == "members" {
		if p.memberIndex(p.selectedID) < 0 {
			p.selectedID = p.current
			if p.memberIndex(p.selectedID) < 0 && len(p.data.Members) > 0 {
				p.selectedID = p.data.Members[0].ID
			}
		}
	} else {
		if p.taskIndex(p.selectedID) < 0 {
			p.detailID = ""
			if tasks := p.visibleTasks(); len(tasks) > 0 {
				p.selectedID = tasks[0].ID
			} else {
				p.selectedID = ""
			}
		}
	}
	p.clampPage()
	if prior != p.selectedID && !p.userPaged {
		if p.kind == "members" {
			p.reveal(p.memberIndex(p.selectedID))
		} else {
			p.reveal(p.taskIndex(p.selectedID))
		}
	}
}

func (p *sidePanel) visibleTasks() []Task {
	var out []Task
	for _, task := range p.data.Tasks {
		if finished(task.State) == p.completed {
			out = append(out, task)
		}
	}
	return out
}

func (p *sidePanel) memberFirst() int {
	if len(p.data.Members) > 0 && p.data.Members[0].ID == "master" {
		return 1
	}
	return 0
}

func (p *sidePanel) memberIndex(id string) int {
	for i, m := range p.data.Members {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func (p *sidePanel) taskIndex(id string) int {
	for i, task := range p.visibleTasks() {
		if task.ID == id {
			return i
		}
	}
	return -1
}

func (p *sidePanel) perPage() int {
	errorRows := 0
	if p.errorText != "" {
		errorRows = 1
	}
	if p.kind == "members" {
		card, gap := p.memberCardHeight(), p.memberGap()
		reserved := p.memberTitleRows() + 1 // team page label
		if p.memberFirst() == 1 {
			reserved += card + gap
			if p.height >= 18 {
				reserved++
			} // LEAD label
		}
		return max(1, (p.height-reserved-errorRows)/(card+gap))
	}
	heading, tabs, count := p.taskHeaderRows()
	return max(1, (p.height-heading-tabs-count-errorRows)/(p.taskCardHeight()+p.taskGap()))
}

func (p *sidePanel) memberCardHeight() int {
	if p.height < 18 {
		return 4
	}
	if p.height < 30 {
		return 6
	}
	return 8
}
func (p *sidePanel) memberGap() int {
	if p.height < 18 {
		return 0
	}
	return 1
}
func (p *sidePanel) memberTitleRows() int {
	if p.height < 30 {
		return 1
	}
	return 2
}
func (p *sidePanel) taskHeaderRows() (heading, tabs, count int) {
	if p.height < 18 {
		return 1, 1, 1
	}
	if p.height < 24 {
		return 1, 2, 1
	}
	return 2, 3, 2
}
func (p *sidePanel) taskCardHeight() int {
	errorRows := 0
	if p.errorText != "" {
		errorRows = 1
	}
	if p.height < 18 {
		return max(5, p.height-3-errorRows)
	}
	if p.height == 18 && errorRows == 1 {
		return 12
	}
	return 13
}
func (p *sidePanel) taskGap() int {
	if p.height < 18 {
		return 0
	}
	if p.height < 24 {
		return 1
	}
	return 2
}

func (p *sidePanel) pageCount() int {
	count := len(p.visibleTasks())
	if p.kind == "members" {
		count = len(p.data.Members) - p.memberFirst()
	}
	return max(1, (count+p.perPage()-1)/p.perPage())
}

func (p *sidePanel) clampPage() { p.page = max(0, min(p.page, p.pageCount()-1)) }

func (p *sidePanel) reveal(index int) {
	if p.kind == "members" {
		index -= p.memberFirst()
	}
	if index >= 0 {
		p.page = index / p.perPage()
	}
	p.clampPage()
}

func (p *sidePanel) key(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
		if len(p.focusOrder) > 0 {
			current := p.app.GetFocus()
			index := -1
			for i, primitive := range p.focusOrder {
				if primitive == current {
					index = i
					break
				}
			}
			if event.Key() == tcell.KeyTab {
				index++
			} else {
				index--
			}
			index = (index + len(p.focusOrder)) % len(p.focusOrder)
			next := p.focusOrder[index]
			if p.kind == "members" {
				for id, card := range p.memberCards {
					if card == next {
						p.selectedID = id
						p.render()
						return nil
					}
				}
			}
			p.app.SetFocus(next)
		}
		return nil
	}
	if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
		if p.detailID != "" && event.Key() != tcell.KeyCtrlC {
			p.detailID = ""
			p.render()
		} else if p.kind == "tasks" {
			p.action(Action{Kind: "close"})
		}
		return nil
	}
	if p.kind == "tasks" && p.detailID != "" {
		return event
	}
	switch event.Key() {
	case tcell.KeyPgDn:
		p.userPaged = true
		p.page = min(p.page+1, p.pageCount()-1)
		p.render()
		return nil
	case tcell.KeyPgUp:
		p.userPaged = true
		p.page = max(0, p.page-1)
		p.render()
		return nil
	case tcell.KeyUp:
		p.move(-1)
		return nil
	case tcell.KeyDown:
		p.move(1)
		return nil
	case tcell.KeyLeft:
		if p.kind == "tasks" {
			p.filter(false)
			return nil
		}
	case tcell.KeyRight:
		if p.kind == "tasks" {
			p.filter(true)
			return nil
		}
	case tcell.KeyEnter:
		if p.kind == "members" {
			p.activateMember()
			return nil
		}
	}
	switch event.Rune() {
	case 'j':
		p.move(1)
		return nil
	case 'k':
		p.move(-1)
		return nil
	case 'o':
		if p.kind == "tasks" {
			if i := p.taskIndex(p.selectedID); i >= 0 {
				p.action(Action{Kind: "open", Member: p.visibleTasks()[i].Owner})
			}
			return nil
		}
	}
	return event
}

func (p *sidePanel) mouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if p.kind == "tasks" && p.detailID != "" {
		return event, action
	}
	if action == tview.MouseScrollDown || action == tview.MouseScrollUp {
		p.userPaged = true
		if action == tview.MouseScrollDown {
			p.page++
		} else {
			p.page--
		}
		p.clampPage()
		p.render()
		return nil, action
	}
	return event, action
}

func (p *sidePanel) move(delta int) {
	p.userPaged = false
	if p.kind == "members" {
		if len(p.data.Members) == 0 {
			return
		}
		i := max(0, p.memberIndex(p.selectedID))
		i = max(0, min(len(p.data.Members)-1, i+delta))
		p.selectedID = p.data.Members[i].ID
		p.reveal(i)
	} else {
		tasks := p.visibleTasks()
		if len(tasks) == 0 {
			return
		}
		i := max(0, p.taskIndex(p.selectedID))
		i = max(0, min(len(tasks)-1, i+delta))
		p.selectedID = tasks[i].ID
		p.reveal(i)
	}
	p.render()
}

func (p *sidePanel) filter(done bool) {
	if p.completed == done {
		return
	}
	p.completed, p.page, p.detailID = done, 0, ""
	p.userPaged = false
	p.selectedID = ""
	p.reconcile()
	p.render()
}

func (p *sidePanel) activateMember() {
	if p.memberIndex(p.selectedID) < 0 {
		return
	}
	id := p.selectedID
	// A session keeps its own member selected after switching away.
	p.selectedID = p.current
	p.userPaged = false
	p.reveal(p.memberIndex(p.current))
	p.render()
	p.action(Action{Kind: "open", Member: id})
}

func (p *sidePanel) action(a Action) {
	if a.Kind == "open" && a.Member == "" {
		return
	}
	go func() {
		message, err := p.act(a)
		p.app.QueueUpdateDraw(func() {
			if err != nil {
				p.errorText = err.Error()
			} else {
				p.errorText = message
			}
			if a.Kind == "close" && err == nil {
				p.app.Stop()
				return
			}
			p.render()
		})
	}()
}

func (p *sidePanel) render() {
	scrollRow, scrollColumn := 0, 0
	if p.detailID != "" && p.detailText != nil {
		scrollRow, scrollColumn = p.detailText.GetScrollOffset()
	}
	p.root.Clear()
	p.focusOrder = nil
	p.root.SetBackgroundColor(uiCanvas)
	p.memberCards = make(map[string]*tview.TextView)
	p.taskButtons = make(map[string]*tview.Button)
	if p.detailID == "" {
		p.detailText = nil
	}
	if p.kind == "members" {
		p.renderMembers()
	} else if p.detailID != "" {
		p.renderDetail()
	} else {
		p.renderTasks()
	}
	if p.errorText != "" {
		p.root.AddItem(textRow("  "+plainLine(p.errorText), uiWarning), 1, 0, false)
	}
	p.app.SetRoot(p.root, true)
	if p.kind == "members" {
		if card := p.memberCards[p.selectedID]; card != nil {
			p.app.SetFocus(card)
		}
	} else if p.detailID != "" {
		if p.detailText != nil {
			p.detailText.ScrollTo(scrollRow, scrollColumn)
			p.app.SetFocus(p.detailText)
		}
	} else if button := p.taskButtons[p.selectedID]; button != nil {
		p.app.SetFocus(button)
	}
}

func textRow(text string, color tcell.Color) *tview.TextView {
	v := tview.NewTextView().SetText(text).SetTextColor(color)
	v.SetBackgroundColor(uiCanvas)
	v.SetWrap(false)
	return v
}

func plainLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 32 || r == 127 {
			return ' '
		}
		return r
	}, clean(s))
	return s
}

func safeLine(s string) string { return tview.Escape(plainLine(s)) }

func safeBody(s string) string {
	rows := strings.Split(clean(s), "\n")
	for i := range rows {
		rows[i] = safeLine(rows[i])
	}
	return strings.Join(rows, "\n")
}

func chip(text, fg, bg string) string {
	return "[" + fg + ":" + bg + ":b] " + safeLine(text) + " [-:-:-]"
}

func stateChip(state string) string {
	name := label(state)
	switch name {
	case "Working", "Ready", "Done":
		return chip(name, "#c8ffe3", "#245e48")
	case "Blocked", "Review", "Awaiting approval":
		return chip(name, "#ffe6ae", "#70491c")
	case "Cancelled":
		return chip(name, "#ffd6d6", "#703b3b")
	default:
		return chip(name, "#eeeeee", "#555555")
	}
}

func taskChips(tasks string) string {
	if strings.TrimSpace(tasks) == "" {
		return chip("No task", "#eeeeee", "#555555")
	}
	var out []string
	for _, id := range strings.Split(tasks, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, chip(id, "#d7efff", "#305270"))
		}
	}
	return strings.Join(out, " ")
}

func memberTaskRows(tasks string) (string, string) {
	if strings.TrimSpace(tasks) == "" {
		return " " + taskChips(""), ""
	}
	ids := strings.Split(tasks, ",")
	first := " " + taskChips(strings.TrimSpace(ids[0]))
	if len(ids) == 1 {
		return first, ""
	}
	second := strings.TrimSpace(ids[1])
	if len(ids) > 2 {
		second += fmt.Sprintf(" +%d", len(ids)-2)
	}
	return first, " " + taskChips(second)
}

func memberMeta(member Member, width int) (string, string) {
	tail := func(text string, room int) string {
		text = clean(text)
		if ansi.StringWidth(text) <= room {
			return text
		}
		return ansi.TruncateLeft(text, ansi.StringWidth(text)-room+1, "…")
	}
	engine := member.Engine
	if engine == "codex" {
		engine = "Codex"
	}
	if engine == "claude" {
		engine = "Claude Code"
	}
	branch := member.Branch
	if branch == "" && member.Commit != "" {
		branch = "detached " + member.Commit
	}
	suffix := ""
	if member.Worktree {
		suffix = " wt"
	}
	room := max(1, width-5-ansi.StringWidth(engine)-len(suffix))
	branch = tail(branch, room)
	path := tail(member.Cwd, max(1, width-2))
	return " " + safeLine(engine) + "  " + safeLine(branch) + suffix, " " + safeLine(path)
}

func (p *sidePanel) memberCard(member Member) *tview.TextView {
	firstTask, secondTask := memberTaskRows(member.Tasks)
	git, path := memberMeta(member, p.width)
	card := tview.NewTextView().SetDynamicColors(true)
	card.SetBackgroundColor(uiCard)
	card.SetTextColor(uiText)
	card.SetBorder(true)
	border := uiMuted
	if member.ID == p.current {
		card.SetBackgroundColor(uiCurrent)
		border = uiBlue
	}
	if member.ID == p.selectedID {
		border = uiAccent
	}
	card.SetBorderColor(border)
	card.SetWrap(false)
	lines := []string{
		" [::b]" + safeLine(member.ID) + "[-:-:-]",
		" " + stateChip(member.State),
		firstTask,
		secondTask,
		git,
		path,
	}
	switch p.memberCardHeight() {
	case 4:
		lines = []string{lines[0], lines[1] + firstTask}
	case 6:
		last := git
		if secondTask != "" {
			last = secondTask
		}
		lines = []string{lines[0], lines[1], firstTask, last}
	}
	card.SetText(strings.Join(lines, "\n"))
	id := member.ID
	card.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if event != nil && card.InRect(event.Position()) && (action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick) {
			p.selectedID = id
			p.activateMember()
			return tview.MouseConsumed, nil
		}
		return action, event
	})
	p.memberCards[id] = card
	p.focusOrder = append(p.focusOrder, card)
	return card
}

func (p *sidePanel) renderMembers() {
	p.root.AddItem(textRow("  MEMBERS", uiText), p.memberTitleRows(), 0, false)
	first := p.memberFirst()
	if first == 1 {
		if p.height >= 18 {
			p.root.AddItem(textRow("  LEAD", uiMuted), 1, 0, false)
		}
		p.root.AddItem(p.memberCard(p.data.Members[0]), p.memberCardHeight(), 0, false)
		if p.memberGap() > 0 {
			p.root.AddItem(nil, p.memberGap(), 0, false)
		}
	}
	p.root.AddItem(textRow(fmt.Sprintf("  TEAM  %d / %d", p.page+1, p.pageCount()), uiMuted), 1, 0, false)
	start := first + p.page*p.perPage()
	end := min(len(p.data.Members), start+p.perPage())
	for _, member := range p.data.Members[start:end] {
		p.root.AddItem(p.memberCard(member), p.memberCardHeight(), 0, false)
		if p.memberGap() > 0 {
			p.root.AddItem(nil, p.memberGap(), 0, false)
		}
	}
	if len(p.data.Members) == 0 {
		p.root.AddItem(textRow("  No members", uiMuted), 2, 0, false)
	}
}

func styledButton(label string, active bool, selected func()) *tview.Button {
	b := tview.NewButton(label)
	// tview classifies a second physical press as DoubleClick globally, while
	// Button handles Click only. Feed each physical press through Button's own
	// hit test and selected callback, including the second press.
	b.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick {
			action = tview.MouseLeftClick
		}
		return action, event
	})
	bg, fg := uiCard, uiText
	if active {
		bg, fg = uiCurrent, uiAccent
	}
	b.SetStyle(tcell.StyleDefault.Background(bg).Foreground(fg))
	b.SetActivatedStyle(tcell.StyleDefault.Background(uiAccent).Foreground(uiCanvas).Bold(true))
	b.SetSelectedFunc(selected)
	return b
}

func (p *sidePanel) renderTasks() {
	headingRows, tabRows, countRows := p.taskHeaderRows()
	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	row.SetBackgroundColor(uiCanvas)
	row.AddItem(textRow("  TASKS", uiText), 0, 1, false)
	closeButton := styledButton("×", false, func() { p.action(Action{Kind: "close"}) })
	p.focusOrder = append(p.focusOrder, closeButton)
	row.AddItem(closeButton, 5, 0, false)
	p.root.AddItem(row, headingRows, 0, false)
	tabs := tview.NewFlex().SetDirection(tview.FlexColumn)
	tabs.SetBackgroundColor(uiCanvas)
	activeCount, doneCount := 0, 0
	for _, task := range p.data.Tasks {
		if finished(task.State) {
			doneCount++
		} else {
			activeCount++
		}
	}
	p.tabs[0] = styledButton(fmt.Sprintf("Active %d", activeCount), !p.completed, func() { p.filter(false) })
	p.tabs[1] = styledButton(fmt.Sprintf("Done %d", doneCount), p.completed, func() { p.filter(true) })
	p.focusOrder = append(p.focusOrder, p.tabs[0], p.tabs[1])
	tabs.AddItem(p.tabs[0], 0, 1, false).AddItem(p.tabs[1], 0, 1, false)
	p.root.AddItem(tabs, tabRows, 0, false)
	tasks := p.visibleTasks()
	p.root.AddItem(textRow(fmt.Sprintf("  %d tasks  ·  %d / %d", len(tasks), p.page+1, p.pageCount()), uiMuted), countRows, 0, false)
	start := p.page * p.perPage()
	end := min(len(tasks), start+p.perPage())
	for _, task := range tasks[start:end] {
		id := task.ID
		card := tview.NewFlex().SetDirection(tview.FlexRow)
		card.SetBackgroundColor(uiCard)
		card.SetBorder(true)
		card.SetBorderColor(uiMuted)
		body := tview.NewTextView().SetDynamicColors(true)
		body.SetBackgroundColor(uiCard)
		body.SetTextColor(uiText)
		done := 0
		for _, step := range task.Milestones {
			if step.complete() {
				done++
			}
		}
		cardLines := []string{
			" " + chip(task.ID, "#d7efff", "#305270") + " " + stateChip(task.State),
			" [::b]" + safeLine(task.Title) + "[-:-:-]",
			" [#a0a0a0]Owner: " + safeLine(task.Owner) + "[-:-:-]",
			fmt.Sprintf(" [#a0a0a0]Milestones %d/%d[-:-:-]", done, len(task.Milestones)),
		}
		if task.Progress != "" {
			cardLines = append(cardLines, " [#a0a0a0]"+safeLine(task.Progress)+"[-:-:-]")
		}
		if task.Note != "" {
			cardLines = append(cardLines, " [#ffdfa6]"+safeLine(task.Note)+"[-:-:-]")
		}
		if task.Completion != "" {
			cardLines = append(cardLines, " [#b7f5ca]"+safeLine(task.Completion)+"[-:-:-]")
		}
		body.SetText(strings.Join(cardLines, "\n"))
		body.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if event != nil && body.InRect(event.Position()) && (action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick) {
				p.selectedID = id
				p.render()
				return tview.MouseConsumed, nil
			}
			return action, event
		})
		card.AddItem(body, 0, 1, false)
		button := styledButton("View details  ›", id == p.selectedID, func() {
			p.selectedID, p.detailID = id, id
			p.render()
		})
		p.taskButtons[id] = button
		p.focusOrder = append(p.focusOrder, button)
		card.AddItem(button, 3, 0, false)
		p.root.AddItem(card, p.taskCardHeight(), 0, false)
		if p.taskGap() > 0 {
			p.root.AddItem(nil, p.taskGap(), 0, false)
		}
	}
	if len(tasks) == 0 {
		p.root.AddItem(textRow("  No tasks in this view", uiMuted), 3, 0, false)
	}
}

func (p *sidePanel) renderDetail() {
	i := p.taskIndex(p.detailID)
	if i < 0 {
		p.detailID = ""
		p.renderTasks()
		return
	}
	task := p.visibleTasks()[i]
	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	row.SetBackgroundColor(uiCanvas)
	row.AddItem(textRow("  TASK DETAILS", uiText), 0, 1, false)
	closeButton := styledButton("×", false, func() { p.action(Action{Kind: "close"}) })
	p.focusOrder = append(p.focusOrder, closeButton)
	row.AddItem(closeButton, 5, 0, false)
	headingRows, _, _ := p.taskHeaderRows()
	p.root.AddItem(row, headingRows, 0, false)
	backButton := styledButton("‹ Back to tasks", false, func() { p.detailID = ""; p.render() })
	p.focusOrder = append(p.focusOrder, backButton)
	backRows := 3
	if p.height < 18 {
		backRows = 1
	}
	p.root.AddItem(backButton, backRows, 0, false)
	lines := []string{
		" " + chip(task.ID, "#d7efff", "#305270") + " " + stateChip(task.State), "",
		" [::b]" + safeLine(task.Title) + "[-:-:-]", "", " Owner: " + safeLine(task.Owner), "",
	}
	if task.Completion != "" {
		lines = append(lines, " "+safeBody(task.Completion), "")
	}
	if task.Note != "" {
		lines = append(lines, " "+safeBody(task.Note), "")
	}
	done := 0
	for _, milestone := range task.Milestones {
		if milestone.complete() {
			done++
		}
	}
	lines = append(lines, fmt.Sprintf(" [::b]MILESTONES  %d/%d[-:-:-]", done, len(task.Milestones)))
	if len(task.Milestones) == 0 {
		lines = append(lines, " No milestones defined")
	}
	for _, milestone := range task.Milestones {
		mark := "○"
		if milestone.complete() {
			mark = "✓"
		} else if milestone.State == "awaiting_approval" || milestone.State == "reported" {
			mark = "◇"
		}
		name := milestone.Name
		if milestone.Gate {
			name += " · approval gate"
		}
		lines = append(lines, " "+mark+" "+safeLine(name)+"  "+safeLine(label(milestone.State)))
	}
	if task.Progress != "" {
		lines = append(lines, "", " [::b]LATEST UPDATE[-:-:-]", " "+safeBody(task.Progress))
	}
	if task.Detail != "" {
		lines = append(lines, "", " [::b]DETAIL[-:-:-]", " "+safeBody(task.Detail))
	}
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetBackgroundColor(uiCard)
	view.SetTextColor(uiText)
	view.SetBorder(true)
	view.SetBorderColor(uiAccent)
	view.SetText(strings.Join(lines, "\n"))
	p.detailText = view
	p.focusOrder = append(p.focusOrder, view)
	p.root.AddItem(view, 0, 1, true)
}
