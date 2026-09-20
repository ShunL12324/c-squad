package teamui

import "time"

// Brief is the state of the user's outstanding brief-report request for a task,
// projected from the ledger. Reading it from there rather than from this process
// is what lets a panel respawn - tmux rebuilds these panes on every layout pass -
// without losing what the user already asked for.
type Brief struct{ MessageID, State, Error string }

// Brief request phases held only in this process, covering the window between a
// press and the ledger catching up.
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

// briefLine describes the request under the card buttons. This process knows the
// most recent press; the ledger knows what survived it, and wins once it does.
func (m model) briefLine(task Task, width int) string {
	if f, ok := m.briefs[task.ID]; ok {
		switch f.phase {
		case briefSending:
			return textStyle(line("· Sending…", width), muted, false)
		case briefFailed:
			return textStyle(line("⚠ Not sent: "+f.text, width), "222", false)
		case briefDone:
			if task.Brief.State == "" {
				return textStyle(line("✓ "+f.text, width), accent, false)
			}
		}
	}
	// A card is about 34 columns wide, so lead with the part that carries the
	// information and let the footer carry the key hints.
	switch task.Brief.State {
	case "":
		return ""
	case "sent":
		return textStyle(line("✓ Asked master · "+task.Brief.MessageID, width), accent, false)
	case "needs_attention":
		return textStyle(line("⚠ "+task.Brief.Error+" · press r", width), "222", false)
	default:
		if task.Brief.Error != "" {
			return textStyle(line("⚠ "+task.Brief.Error, width), "222", false)
		}
		return textStyle(line("· Queued · "+task.Brief.MessageID, width), muted, false)
	}
}
