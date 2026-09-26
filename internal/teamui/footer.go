package teamui

import "strings"

func (m model) footer() []string {
	if m.kind == "tasks" {
		if m.err != nil {
			return []string{"", "  " + textStyle(line(m.err.Error(), m.width-2), "222", false)}
		}
		return nil
	}
	hint := textStyle("↑↓", foreground, false) + textStyle(" Select   ", muted, false) + textStyle("↵", foreground, false) + textStyle(" Open", muted, false)
	// The sidebar rarely holds focus, so name the keys that switch member from
	// anywhere ahead of the ones that only move this panel's cursor.
	if m.kind == "members" && m.data.Switch != "" {
		hint = textStyle(m.data.Switch, foreground, false) + textStyle(" Switch   ", muted, false) + textStyle("↵", foreground, false) + textStyle(" Open", muted, false)
	}
	if m.err != nil {
		hint = textStyle(line(m.err.Error(), m.width-2), "222", false)
	}
	return []string{textStyle("  "+strings.Repeat("─", max(0, m.width-4)), "238", false), "  " + hint}
}
