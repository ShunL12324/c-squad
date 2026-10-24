package statusline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The usage cell. The status line takes a reading of each task's fresh
// tokens (input + cache writes + output) per refresh and keeps the last
// MaxSamples of them per task in the cache directory. Working rows show the
// change between consecutive readings as a trail on a log scale, newest on
// the right; other rows show their total.

const (
	MaxSamples = 9               // readings kept per task: 8 changes
	MinGap     = 8 * time.Second // a reading closer to the previous one is dropped
	StallAfter = 2 * time.Minute // no activity for this long shows as a stall
	trailCells = MaxSamples - 1
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

// unsized is a step in which the session changed but the size is unknown.
const unsized = -1

// steps are the changes between consecutive samples, oldest first: tokens,
// 0 for nothing, unsized for a change of unknown size.
func steps(samples []Sample) []int64 {
	var out []int64
	for i := 1; i < len(samples); i++ {
		a, b := samples[i-1], samples[i]
		switch {
		case a.Known && b.Known && b.Fresh > a.Fresh:
			out = append(out, b.Fresh-a.Fresh)
		case !(a.Known && b.Known) && b.Activity > a.Activity:
			out = append(out, unsized)
		default:
			out = append(out, 0)
		}
	}
	return out
}

var bars = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// step is one trail cell: ▁ (< 500 tokens) to █ (>= 32k), doubling per
// level; · for nothing, ▪ for a change of unknown size.
func step(n int64) string {
	switch {
	case n == unsized:
		return "▪"
	case n <= 0:
		return "·"
	}
	level := 0
	for limit := int64(500); n >= limit && level < len(bars)-1; limit *= 2 {
		level++
	}
	return bars[level]
}

// Trail is the trail of samples as text, oldest first.
func Trail(samples []Sample) string {
	var b strings.Builder
	for _, s := range steps(samples) {
		b.WriteString(step(s))
	}
	return b.String()
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

// trailState: rows whose worker is at work show a trail; the others their
// total.
func trailState(key string) bool { return key == Working || key == Fixing || key == Merging }

// showsUsage: the row has a usage cell (an urgent badge runs across it; a
// waiting task has no session).
func showsUsage(e entry) bool {
	return e.t.Usage != nil && !e.st.urgent && e.t.State != Waiting
}

// usageCell is the usage cell of an entry, w cells wide (right-aligned), or
// nil when the row shows none.
func usageCell(e entry, w int, now time.Time, p painter) line {
	u := e.t.Usage
	if w <= 0 || !showsUsage(e) {
		return nil
	}
	if !trailState(e.t.State) {
		text := "?"
		if u.Known {
			text = Tokens(u.Total)
		}
		if width(text) > w {
			return nil
		}
		return line{{padLeft(text, w), "2"}}
	}
	if !u.Activity.IsZero() && now.Sub(u.Activity) >= StallAfter {
		// A stall: dots ending in the idle time. Only a look: it changes no
		// state and no ordering (a long build writes nothing either).
		idle := Age(u.Activity, now)
		var l line
		if dots := w - 1 - width(idle); dots >= 1 {
			l = line{{strings.Repeat("·", dots) + " ", "2"}}
		} else if width(idle) > w {
			return line{{padLeft("·", w), "2"}}
		} else {
			l = line{{strings.Repeat(" ", w-width(idle)), ""}}
		}
		return append(l, p.fg(amber, idle))
	}
	st := steps(u.Samples)
	if len(st) > w {
		st = st[len(st)-w:]
	}
	l := line{{strings.Repeat(" ", w-len(st)), ""}}
	for _, s := range st {
		if s == 0 {
			l = append(l, segment{"·", "2"})
		} else {
			l = append(l, p.fg(e.st.rgb, step(s)))
		}
	}
	return l
}
