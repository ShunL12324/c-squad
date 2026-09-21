package teamui

import "time"

// Brief feedback is local to this panel and is not a durable delivery receipt.
const (
	briefSending = "sending"
	briefDone    = "done"
	briefFailed  = "failed"
)

// briefDebounce drops repeat presses of the same button. tmux forwards
// MouseDown1Pane, SecondClick1Pane, DoubleClick1Pane and TripleClick1Pane to the
// panel, so one physical double click arrives here as several presses.
const briefDebounce = 400 * time.Millisecond

type briefFeedback struct {
	phase, text string
	at          time.Time
}

// press reports whether a new request may start, and records the attempt.
func (m model) press(id string) bool {
	last, seen := m.briefs[id]
	if seen && (last.phase == briefSending || time.Since(last.at) < briefDebounce) {
		return false
	}
	m.briefs[id] = briefFeedback{phase: briefSending, at: time.Now()}
	return true
}

// briefLine reports only this panel's latest native send attempt.
func (m model) briefLine(task Task, width int) string {
	if f, ok := m.briefs[task.ID]; ok {
		switch f.phase {
		case briefSending:
			return textStyle(line("· Sending…", width), muted, false)
		case briefFailed:
			return textStyle(line("⚠ Send failed: "+f.text, width), "222", false)
		case briefDone:
			return textStyle(line("✓ "+f.text, width), accent, false)
		}
	}
	return ""
}
