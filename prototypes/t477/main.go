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
	app     *tview.Application
	root    tview.Primitive
	members []*tview.TextView
	buttons []*tview.Button
	status  *tview.TextView
}

func newDemo(panel string) *demo {
	d := &demo{app: tview.NewApplication()}
	left := d.memberPanel()
	right := d.taskPanel()
	switch panel {
	case "members":
		d.root = left
	case "tasks":
		d.root = right
	default:
		d.root = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(left, memberWidth, 0, true).
			AddItem(right, 0, 1, false)
	}
	d.app.SetRoot(d.root, true).EnableMouse(true)
	if panel == "tasks" {
		d.app.SetFocus(d.buttons[0])
	} else {
		d.app.SetFocus(d.members[1])
	}
	d.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC || event.Rune() == 'q' {
			d.app.Stop()
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

func (d *demo) memberPanel() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBackgroundColor(canvas)
	header := tview.NewTextView().SetText("  MEMBERS    1 / 1\n  Current: navigation-dev")
	header.SetTextColor(muted)
	header.SetBackgroundColor(canvas)
	panel.AddItem(header, 3, 0, false)
	cards := []struct {
		name, state, task, git, dir string
		color, bg                   tcell.Color
	}{
		{"◆ master", "Working · Codex", "Task T477 · UI direction", "Git main", "Dir ~/projects/c-squad", accent, surface},
		{"● navigation-dev", "Working · Codex", "Task T477 · prototype", "Git csquad/T477  wt", "Dir …/worktrees/T477", blue, selected},
		{"design-review", "Review · Codex", "Task T451 · approved", "Git main", "Dir ~/projects/c-squad", accent, surface},
		{"release-check", "Idle · Codex", "No task", "Git main", "Dir ~/projects/c-squad", muted, surface},
	}
	for i, item := range cards {
		body := tview.NewTextView().SetDynamicColors(true)
		body.SetText(fmt.Sprintf(" [::b]%s[-:-:-]\n %s\n %s\n %s\n %s", item.name, item.state, item.task, item.git, item.dir))
		body.SetTextColor(text)
		body.SetBackgroundColor(item.bg)
		body.SetBorder(true)
		body.SetBorderColor(item.color)
		body.SetFocusFunc(func() { body.SetBorderColor(accent) })
		body.SetBlurFunc(func() { body.SetBorderColor(item.color) })
		body.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftDown {
				d.app.SetFocus(body)
			}
			return action, event
		})
		d.members = append(d.members, body)
		panel.AddItem(body, 7, 0, i == 1)
		panel.AddItem(nil, 1, 0, false)
	}
	footer := tview.NewTextView().SetText("  ↑↓ focus · click · q quit")
	footer.SetTextColor(muted)
	footer.SetBackgroundColor(canvas)
	panel.AddItem(footer, 0, 1, false)
	return panel
}

func (d *demo) taskPanel() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBackgroundColor(canvas)
	header := tview.NewTextView().SetText("  TASKS  /  In progress     Done\n  2 active tasks")
	header.SetTextColor(muted)
	header.SetBackgroundColor(canvas)
	panel.AddItem(header, 3, 0, false)
	data := []struct{ id, title, owner, state, update string }{
		{"T477", "Adopt reusable TUI controls", "navigation-dev", "Working", "Button and card prototype"},
		{"T470", "Publish and verify v0.12.5", "master", "Done", "All channels verified"},
	}
	for i, task := range data {
		card := tview.NewFlex().SetDirection(tview.FlexRow)
		card.SetBackgroundColor(surface)
		card.SetBorder(true)
		card.SetBorderColor(muted)
		content := tview.NewTextView().SetDynamicColors(true)
		content.SetBackgroundColor(surface)
		content.SetTextColor(text)
		content.SetText(fmt.Sprintf(" [#73d7af::b]%s[-:-:-]   %s\n\n [::b]%s[-:-:-]\n\n Owner  %s\n Update %s", task.id, task.state, task.title, task.owner, task.update))
		card.AddItem(content, 7, 0, false)
		button := tview.NewButton("View details  ›")
		button.SetStyle(tcell.StyleDefault.Foreground(accent).Background(selected))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(canvas).Background(accent).Bold(true))
		button.SetBorder(true)
		button.SetBorderColor(accent)
		id := task.id
		button.SetSelectedFunc(func() { d.status.SetText("  Opened " + id + " via tview Button") })
		button.SetExitFunc(func(key tcell.Key) {
			if key == tcell.KeyTab {
				d.app.SetFocus(d.buttons[(i+1)%len(d.buttons)])
			} else if key == tcell.KeyBacktab {
				d.app.SetFocus(d.buttons[(i+len(d.buttons)-1)%len(d.buttons)])
			}
		})
		d.buttons = append(d.buttons, button)
		card.AddItem(button, 3, 0, i == 0)
		panel.AddItem(card, 13, 0, false)
		panel.AddItem(nil, 1, 0, false)
	}
	d.status = tview.NewTextView().SetText("  Tab focus · Enter/click activate")
	d.status.SetTextColor(muted)
	d.status.SetBackgroundColor(canvas)
	panel.AddItem(d.status, 0, 1, false)
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

func main() {
	panel := flag.String("panel", "both", "members, tasks, or both")
	preview := flag.Bool("snapshot", false, "render a 46-row snapshot without a terminal")
	plain := flag.Bool("plain", false, "omit ANSI color from the snapshot")
	flag.Parse()
	if *panel != "members" && *panel != "tasks" && *panel != "both" {
		fmt.Fprintln(os.Stderr, "panel must be members, tasks, or both")
		os.Exit(2)
	}
	d := newDemo(*panel)
	if *preview {
		width := memberWidth + taskWidth
		if *panel == "members" {
			width = memberWidth
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
