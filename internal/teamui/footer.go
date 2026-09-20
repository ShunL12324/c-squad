package teamui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// footerButton shares its rendered cell bounds with pointer hit testing.
type footerButton struct {
	label, key, action string
	start, end         int
}

func (m model) footerButtons() []footerButton {
	var buttons []footerButton
	if m.detail {
		buttons = append(buttons, footerButton{label: "Back", key: "Esc", action: "back"})
	}
	x := 1
	for i := range buttons {
		buttons[i].start = x
		x += ansi.StringWidth(buttons[i].label) + ansi.StringWidth(buttons[i].key) + 4
		buttons[i].end = x
		x++
	}
	return buttons
}

func (m model) footer() []string {
	hint := textStyle("↑↓", foreground, false) + textStyle(" Select   ", muted, false) + textStyle("↵", foreground, false) + textStyle(" Open", muted, false)
	// The sidebar rarely holds focus, so name the keys that switch member from
	// anywhere ahead of the ones that only move this panel's cursor.
	if m.kind == "members" && m.data.Switch != "" {
		hint = textStyle(m.data.Switch, foreground, false) + textStyle(" Switch   ", muted, false) + textStyle("↵", foreground, false) + textStyle(" Open", muted, false)
	}
	if m.kind == "tasks" {
		hint = textStyle("←→", foreground, false) + textStyle(" Filter  ", muted, false) + textStyle("↵", foreground, false) + textStyle(" Details  ", muted, false) + textStyle("b", foreground, false) + textStyle(" Brief  ", muted, false) + textStyle("g", foreground, false) + textStyle(" Master", muted, false)
		if m.detail {
			hint = textStyle("↑↓", foreground, false) + textStyle(" Scroll  ", muted, false) + textStyle("PgUp/Dn", foreground, false) + textStyle(" Page  ", muted, false) + textStyle("b", foreground, false) + textStyle(" Brief", muted, false)
		}
	}
	if m.err != nil {
		hint = textStyle(line(m.err.Error(), m.width-2), "222", false)
	}
	if len(m.footerButtons()) == 0 {
		return []string{textStyle("  "+strings.Repeat("─", max(0, m.width-4)), "238", false), "  " + hint}
	}
	var buttons strings.Builder
	buttons.WriteString(" ")
	for i, b := range m.footerButtons() {
		if i > 0 {
			buttons.WriteString(" ")
		}
		style := lipgloss.NewStyle().Background(lipgloss.Color(brandSurface))
		buttons.WriteString(style.Foreground(lipgloss.Color(foreground)).Render(" " + b.label + "  "))
		buttons.WriteString(style.Foreground(lipgloss.Color(muted)).Render(b.key + " "))
	}
	return []string{" " + hint, buttons.String()}
}
