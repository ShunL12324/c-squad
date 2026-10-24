package statusline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The usage cell. The status line takes a reading of each task's fresh
// tokens (input + cache writes + output) per refresh and keeps the last
// MaxSamples of them per task in the cache directory. Rows whose worker is
// at it show the total and the change between the last two readings
// (`326k +4.1k`); other rows show their total.

const (
	MaxSamples = 2               // readings kept per task: the change since the last refresh
	MinGap     = 8 * time.Second // a reading closer to the previous one is dropped
	StallAfter = 2 * time.Minute // no activity for this long shows as a stall
)

// Sample is one reading of a task's usage.
type Sample struct {
	At       int64 `json:"at"` // unix milliseconds
	Fresh    int64 `json:"fresh"`
	Known    bool  `json:"known,omitempty"`    // the session recorded usage
	Activity int64 `json:"activity,omitempty"` // the session's last write, unix milliseconds
}

// Usage is what a row's usage cell shows.
type Usage struct {
	Samples  []Sample // oldest first; the newest is the current reading
	Known    bool     // Total is known
	Total    int64    // fresh tokens
	Activity time.Time
}

// AddSample appends s, unless the newest reading is less than MinGap older,
// and keeps the newest MaxSamples.
func AddSample(list []Sample, s Sample) []Sample {
	if n := len(list); n > 0 && s.At-list[n-1].At < MinGap.Milliseconds() {
		return list
	}
	list = append(list, s)
	if len(list) > MaxSamples {
		list = list[len(list)-MaxSamples:]
	}
	return list
}

// Unsized is a change in which the session changed but the size is unknown
// (a gateway that reports no or partial usage).
const Unsized = -1

// Change is the change between the last two samples: tokens, 0 for
// nothing, Unsized for a change of unknown size; ok is false with fewer than
// two samples. span is the time between them.
func Change(samples []Sample) (n int64, span time.Duration, ok bool) {
	if len(samples) < 2 {
		return 0, 0, false
	}
	a, b := samples[len(samples)-2], samples[len(samples)-1]
	span = time.Duration(b.At-a.At) * time.Millisecond
	switch {
	case a.Known && b.Known && b.Fresh > a.Fresh:
		return b.Fresh - a.Fresh, span, true
	case !(a.Known && b.Known) && b.Activity > a.Activity:
		return Unsized, span, true
	}
	return 0, span, true
}

// ChangeText is a change as the ledger shows it: +4.1k, +0, +?.
func ChangeText(n int64) string {
	if n == Unsized {
		return "+?"
	}
	return "+" + Tokens(max(0, n))
}

// Tokens is a count in at most four cells, for the ledger: 622, 4.1k,
// 326k, 1.2M, 12M.
func Tokens(n int64) string {
	f := float64(n)
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 9950:
		return fmt.Sprintf("%.1fk", f/1e3)
	case n < 999500:
		return fmt.Sprintf("%.0fk", f/1e3)
	case n < 9950000:
		return fmt.Sprintf("%.1fM", f/1e6)
	}
	return fmt.Sprintf("%.0fM", f/1e6)
}

// TokensExact is a count with three significant digits: 622, 51.8k, 221k,
// 3.05M.
func TokensExact(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	v, unit := float64(n)/1e3, "k"
	if n >= 999500 {
		v, unit = float64(n)/1e6, "M"
	}
	switch {
	case v < 9.995:
		return fmt.Sprintf("%.2f%s", v, unit)
	case v < 99.95:
		return fmt.Sprintf("%.1f%s", v, unit)
	}
	return fmt.Sprintf("%.0f%s", v, unit)
}

// samplesFile keeps a task's samples, with the session they were read from.
func samplesFile(dir string, id int) string {
	return filepath.Join(dir, fmt.Sprintf("T%d.usage.json", id))
}

type samplesOf struct {
	Session string   `json:"session"`
	Samples []Sample `json:"samples"`
}

// LoadSamples returns the samples the status line kept for task id's
// session (none when they are from another session).
func LoadSamples(dir string, id int, session string) []Sample {
	var s samplesOf
	if b, err := os.ReadFile(samplesFile(dir, id)); err != nil || json.Unmarshal(b, &s) != nil || s.Session != session {
		return nil
	}
	return s.Samples
}

func saveSamples(dir string, id int, session string, samples []Sample) {
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	b, _ := json.Marshal(samplesOf{session, samples})
	f := samplesFile(dir, id)
	tmp := f + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, f) != nil {
		os.Remove(tmp)
	}
}

// workingState: rows whose worker is at it show the change since the last
// refresh after their total; the others their total only.
func workingState(key string) bool {
	return key == Working || key == Checking || key == Fixing || key == Merging
}

// showsUsage: the row has a usage cell (an urgent badge runs across it; a
// waiting task has no session).
func showsUsage(e entry) bool {
	return e.t.Usage != nil && !e.st.urgent && e.t.State != Waiting
}

// changeCells is the least width of the change part on rows that show one:
// +4.1k, +0, +? and idle 3m fit, so a stall does not move the row (idle
// 12m takes one cell more).
const changeCells = 7

// showsChange: the row shows a change part after its total, once it has two
// readings.
func showsChange(e entry) bool { return showsUsage(e) && workingState(e.t.State) }

// totalPart is the total of a row: faint unless the worker is at it, ? when
// it is not known; empty when the row shows no usage cell.
func totalPart(e entry) segment {
	if !showsUsage(e) {
		return segment{}
	}
	text := "?"
	if e.t.Usage.Known {
		text = Tokens(e.t.Usage.Total)
	}
	if workingState(e.t.State) {
		return segment{text, ""}
	}
	return segment{text, "2"}
}

// changePart is what follows the total of a working row: the change since
// the last refresh (+4.1k in the state's colour, +0 faint, +? for a change
// of unknown size), or after StallAfter without activity the idle time in
// amber. A stall is only a look: it changes no state and no ordering (a long
// build writes nothing either). A checking task's worker waits for the check
// by design, so it shows no stall. "" when there is nothing to show yet.
func changePart(e entry, now time.Time, p painter) segment {
	u := e.t.Usage
	if !showsUsage(e) || !workingState(e.t.State) {
		return segment{}
	}
	if e.t.State != Checking && !u.Activity.IsZero() && now.Sub(u.Activity) >= StallAfter {
		return p.fg(amber, "idle "+Age(u.Activity, now))
	}
	n, _, ok := Change(u.Samples)
	switch {
	case !ok:
		return segment{}
	case n == 0:
		return segment{"+0", "2"}
	}
	return p.fg(e.st.rgb, ChangeText(n))
}

// usageCell is the usage cell of a row in a column of field widths w: the
// total right-aligned in its field, then, when the column has a change
// field, a space and the change part (when change is set) left-aligned in
// it; blank when the row shows none.
func usageCell(e entry, w fields, change bool, now time.Time, p painter) line {
	total := totalPart(e)
	if total.text == "" {
		return line{spaces(w.usage())}
	}
	l := line{spaces(w.total - width(total.text)), total}
	if w.change == 0 {
		return l
	}
	part := segment{}
	if change {
		part = changePart(e, now, p)
	}
	return append(l, segment{" ", ""}, part, spaces(w.change-width(part.text)))
}
