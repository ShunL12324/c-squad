// This isolated prototype compares real tview controls with the current
// Bubble Tea side panels. It is not wired into the product.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	memberWidth = 32
	taskWidth   = 40
	panelHeight = 46
)

var (
	canvas   = tcell.PaletteColor(234)
	surface  = tcell.PaletteColor(236)
	selected = tcell.PaletteColor(238)
	text     = tcell.PaletteColor(253)
	muted    = tcell.PaletteColor(245)
	accent   = tcell.PaletteColor(115)
	blue     = tcell.PaletteColor(81)
)

type demo struct {
	app         *tview.Application
	root        tview.Primitive
	panel, view string
	leftWidth   int
	members     []*tview.TextView
	buttons     []*tview.Button
	tabs        []*tview.Button
	status      *tview.TextView
}

func newDemo(panel, view string, leftWidth int) *demo {
	d := &demo{app: tview.NewApplication(), panel: panel, view: view, leftWidth: leftWidth}
	d.rebuild()
	d.app.EnableMouse(true)
	d.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC || event.Rune() == 'q' {
			d.app.Stop()
			return nil
		}
		if d.panel != "members" && (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) {
			if event.Key() == tcell.KeyLeft {
				d.switchView("active")
			} else {
				d.switchView("done")
			}
			return nil
		}
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			focus := d.app.GetFocus()
			for i, card := range d.members {
				if focus == card {
					next := i - 1
					if event.Key() == tcell.KeyDown {
						next = i + 1
					}
					if next >= 0 && next < len(d.members) {
						d.app.SetFocus(d.members[next])
					}
					return nil
				}
			}
		}
		return event
	})
	return d
}

func (d *demo) rebuild() {
	d.members, d.buttons, d.tabs = nil, nil, nil
	left := d.memberPanel()
	right := d.taskPanel()
	switch d.panel {
	case "members":
		d.root = left
	case "tasks":
		d.root = right
	default:
		d.root = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(left, d.leftWidth, 0, true).
			AddItem(right, 0, 1, false)
	}
	d.app.SetRoot(d.root, true)
	if d.panel == "tasks" {
		d.app.SetFocus(d.buttons[0])
	} else {
		// Keep current-session styling visible while another card has focus.
		d.app.SetFocus(d.members[2])
	}
}

func (d *demo) switchView(view string) {
	if d.view == view {
		return
	}
	d.view = view
	d.rebuild()
	if len(d.buttons) > 0 {
		d.app.SetFocus(d.buttons[0])
	}
}

type memberExample struct {
	name, state, engine, summary, git, dir string
	tasks                                  []string
	current                                bool
}

type taskExample struct {
	id, title, owner, state, update, action string
	disabled                                bool
}

func chip(value, fg, bg string) string {
	return "[" + fg + ":" + bg + ":b] " + value + " [-:-:-]"
}

func stateChip(state string) string {
	switch state {
	case "Working":
		return chip(state, "#c8ffe3", "#245e48")
	case "Blocked":
		return chip(state, "#ffe6ae", "#70491c")
	default:
		return chip(state, "#eeeeee", "#555555")
	}
}

func taskChips(tasks []string) string {
	if len(tasks) == 0 {
		return chip("No task", "#e5e5e5", "#484848")
	}
	var out []string
	for _, task := range tasks {
		out = append(out, chip(task, "#d7efff", "#305270"))
	}
	return strings.Join(out, " ")
}

func (d *demo) memberPanel() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBackgroundColor(canvas)
	header := tview.NewTextView().SetText("  MEMBERS     1 / 1")
	header.SetTextColor(muted)
	header.SetBackgroundColor(canvas)
	panel.AddItem(header, 2, 0, false)
	cards := []memberExample{
		{"◆ master", "Working", "Codex", "UI direction", "Git main", "Dir ~/projects/c-squad", []string{"T477"}, false},
		{"navigation-dev", "Working", "Codex", "Cards + header", "Git csquad/T477 wt", "Dir …/worktrees/T477", []string{"T477", "T480"}, true},
		{"design-review", "Blocked", "Codex", "Awaiting approval", "Git main", "Dir ~/projects/c-squad", []string{"T451"}, false},
		{"release-check", "Idle", "Codex", "Available", "Git main", "Dir ~/projects/c-squad", nil, false},
	}
	for _, item := range cards {
		body := tview.NewTextView().SetDynamicColors(true)
		body.SetText(fmt.Sprintf(" [::b]%s[-:-:-]\n %s %s\n %s · %s\n %s\n %s",
			item.name, stateChip(item.state), taskChips(item.tasks), item.engine, item.summary, item.git, item.dir))
		body.SetTextColor(text)
		bg, edge := surface, muted
		if item.current {
			bg, edge = selected, blue
		}
		body.SetBackgroundColor(bg)
		body.SetBorder(true)
		body.SetBorderColor(edge)
		body.SetFocusFunc(func() { body.SetBorderColor(accent) })
		body.SetBlurFunc(func() { body.SetBorderColor(edge) })
		body.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftDown {
				d.app.SetFocus(body)
			}
			return action, event
		})
		d.members = append(d.members, body)
		panel.AddItem(body, 8, 0, false)
		panel.AddItem(nil, 1, 0, false)
	}
	return panel
}

func (d *demo) taskPanel() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBackgroundColor(canvas)
	header := tview.NewTextView().SetText("  TASKS")
	header.SetTextColor(muted)
	header.SetBackgroundColor(canvas)
	panel.AddItem(header, 1, 0, false)
	tabs := tview.NewFlex().SetDirection(tview.FlexColumn)
	tabs.SetBackgroundColor(canvas)
	for _, view := range []string{"active", "done"} {
		name := "Active"
		if view == "done" {
			name = "Done"
		}
		button := tview.NewButton(name)
		button.SetBorder(true)
		button.SetBorderColor(muted)
		button.SetStyle(tcell.StyleDefault.Foreground(muted).Background(surface))
		if view == d.view {
			button.SetStyle(tcell.StyleDefault.Foreground(accent).Background(selected).Bold(true))
			button.SetBorderColor(accent)
		}
		selectedView := view
		button.SetSelectedFunc(func() { d.switchView(selectedView) })
		d.tabs = append(d.tabs, button)
		tabs.AddItem(button, 0, 1, false)
	}
	panel.AddItem(tabs, 3, 0, false)
	d.status = tview.NewTextView()
	d.status.SetTextColor(muted)
	d.status.SetBackgroundColor(canvas)
	data := []taskExample{
		{"T477", "Reusable TUI controls", "navigation-dev", "Working", "Member badges + buttons", "View details  ›", false},
		{"T480", "Keep header height fixed", "master", "Working", "Native resize review", "View details  ›", false},
		{"T481", "Waiting for approval", "master", "Blocked", "Disabled-state example", "Awaiting approval", true},
	}
	if d.view == "done" {
		data = []taskExample{
			{"T470", "Publish v0.12.5", "master", "Done", "All channels verified", "View details  ›", false},
			{"T302", "Release v0.12.3", "master", "Cancelled", "Superseded by v0.12.4", "View details  ›", false},
		}
	}
	d.status.SetText(fmt.Sprintf("  %d %s tasks", len(data), d.view))
	panel.AddItem(d.status, 1, 0, false)
	for i, task := range data {
		card := tview.NewFlex().SetDirection(tview.FlexRow)
		card.SetBackgroundColor(surface)
		card.SetBorder(true)
		card.SetBorderColor(muted)
		content := tview.NewTextView().SetDynamicColors(true)
		content.SetBackgroundColor(surface)
		content.SetTextColor(text)
		content.SetText(fmt.Sprintf(" [#73d7af::b]%s[-:-:-]   %s\n\n [::b]%s[-:-:-]\n Owner  %s\n Update %s", task.id, task.state, task.title, task.owner, task.update))
		card.AddItem(content, 6, 0, false)
		button := tview.NewButton(task.action)
		button.SetStyle(tcell.StyleDefault.Foreground(accent).Background(selected))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(canvas).Background(accent).Bold(true))
		button.SetDisabledStyle(tcell.StyleDefault.Foreground(muted).Background(surface))
		button.SetDisabled(task.disabled)
		button.SetBorder(true)
		button.SetBorderColor(muted)
		id := task.id
		button.SetSelectedFunc(func() { d.status.SetText("  Opened " + id + " via tview Button") })
		button.SetExitFunc(func(key tcell.Key) {
			step := 0
			if key == tcell.KeyTab {
				step = 1
			} else if key == tcell.KeyBacktab {
				step = -1
			}
			for offset := 1; step != 0 && offset <= len(d.buttons); offset++ {
				next := (i + step*offset + len(d.buttons)*2) % len(d.buttons)
				if !d.buttons[next].IsDisabled() {
					d.app.SetFocus(d.buttons[next])
					break
				}
			}
		})
		d.buttons = append(d.buttons, button)
		card.AddItem(button, 3, 0, i == 0)
		panel.AddItem(card, 12, 0, false)
		panel.AddItem(nil, 1, 0, false)
	}
	return panel
}

func snapshot(d *demo, width int, plain bool) error {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetSize(width, panelHeight)
	d.root.SetRect(0, 0, width, panelHeight)
	d.root.Draw(screen)
	for y := 0; y < panelHeight; y++ {
		lastFG, lastBG := tcell.ColorDefault, tcell.ColorDefault
		lastAttrs := tcell.AttrMask(0)
		var row strings.Builder
		for x := 0; x < width; x++ {
			ch, _, style, _ := screen.GetContent(x, y)
			fg, bg, attrs := style.Decompose()
			if !plain && (fg != lastFG || bg != lastBG || attrs != lastAttrs) {
				bold := "22"
				if attrs&tcell.AttrBold != 0 {
					bold = "1"
				}
				row.WriteString("\x1b[" + bold + ";" + colorSGR(fg, true, 253) + ";" + colorSGR(bg, false, 234) + "m")
				lastFG, lastBG = fg, bg
				lastAttrs = attrs
			}
			if ch == 0 {
				ch = ' '
			}
			row.WriteRune(ch)
		}
		if !plain {
			row.WriteString("\x1b[0m")
		}
		fmt.Println(row.String())
	}
	return nil
}

func colorSGR(color tcell.Color, foreground bool, fallback int) string {
	prefix := 38
	if !foreground {
		prefix = 48
	}
	if color.IsRGB() {
		r, g, b := color.RGB()
		return fmt.Sprintf("%d;2;%d;%d;%d", prefix, r, g, b)
	}
	for i := 0; i < 256; i++ {
		if color == tcell.PaletteColor(i) {
			return fmt.Sprintf("%d;5;%d", prefix, i)
		}
	}
	return fmt.Sprintf("%d;5;%d", prefix, fallback)
}

// flow invokes the library's actual handlers on a simulated screen. This
// gives a reproducible callback trace without a live terminal or product tests.
func flow() error {
	d := newDemo("tasks", "active", memberWidth)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetSize(taskWidth, panelHeight)
	d.root.SetRect(0, 0, taskWidth, panelHeight)
	d.root.Draw(screen)
	fmt.Printf("initial: focused=%t unfocused=%t disabled=%t\n",
		d.buttons[0].HasFocus(), !d.buttons[1].HasFocus(), d.buttons[2].IsDisabled())
	focus := func(p tview.Primitive) { d.app.SetFocus(p) }
	d.buttons[0].InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), focus)
	fmt.Println("Enter:", strings.TrimSpace(d.status.GetText(true)))
	click := func(b *tview.Button) {
		x, y, _, _ := b.GetRect()
		event := tcell.NewEventMouse(x+1, y+1, tcell.Button1, tcell.ModNone)
		b.MouseHandler()(tview.MouseLeftDown, event, focus)
		b.MouseHandler()(tview.MouseLeftClick, event, focus)
	}
	click(d.buttons[1])
	fmt.Println("click:", strings.TrimSpace(d.status.GetText(true)))
	before := d.status.GetText(true)
	click(d.buttons[2])
	d.buttons[2].InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), focus)
	fmt.Printf("disabled click/Enter: callback unchanged=%t\n", before == d.status.GetText(true))
	click(d.tabs[1])
	fmt.Printf("Done tab click: view=%s, visible actions=%d\n", d.view, len(d.buttons))
	return nil
}

func main() {
	panel := flag.String("panel", "both", "members, tasks, or both")
	view := flag.String("view", "active", "active or done task tab")
	leftWidth := flag.Int("member-width", memberWidth, "member pane width (28 or 32 for review)")
	preview := flag.Bool("snapshot", false, "render a 46-row snapshot without a terminal")
	plain := flag.Bool("plain", false, "omit ANSI color from the snapshot")
	showFlow := flag.Bool("flow", false, "show actual Button handler callbacks")
	flag.Parse()
	if *panel != "members" && *panel != "tasks" && *panel != "both" {
		fmt.Fprintln(os.Stderr, "panel must be members, tasks, or both")
		os.Exit(2)
	}
	if *view != "active" && *view != "done" {
		fmt.Fprintln(os.Stderr, "view must be active or done")
		os.Exit(2)
	}
	if *leftWidth < 28 || *leftWidth > 32 {
		fmt.Fprintln(os.Stderr, "member-width must be between 28 and 32")
		os.Exit(2)
	}
	if *showFlow {
		if err := flow(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	d := newDemo(*panel, *view, *leftWidth)
	if *preview {
		width := *leftWidth + taskWidth
		if *panel == "members" {
			width = *leftWidth
		} else if *panel == "tasks" {
			width = taskWidth
		}
		if err := snapshot(d, width, *plain); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := d.app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
