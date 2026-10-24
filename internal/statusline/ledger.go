// Package statusline is the status line wrapper: the user's own status line,
// then a ledger of this session's tasks.
package statusline

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Task is one row of the ledger.
type Task struct {
	ID    int
	Mark  string // engine mark (✻ Claude, ⬡ Codex); "" when only one engine exists
	Title string
	State string // a state key (see styles); unknown keys rank with idle
	Text  string // the state as shown; "" = the key's own text
	Note  string // for needs_you: why
	Since time.Time
	Usage *Usage // nil: no usage cell (no session, or no reading)
}

// State keys.
const (
	NeedsInput = "needs_input"
	NeedsYou   = "needs_you"
	Failed     = "failed"
	Ready      = "ready"
	Done       = "done"
	Merging    = "merging"
	Fixing     = "fixing"
	Checking   = "checking"
	Working    = "working"
	Idle       = "idle"
	Waiting    = "waiting"
	Stopped    = "stopped"
	Gone       = "gone" // the session was removed outside csquad
)

type style struct {
	glyph  string
	rgb    string
	rank   int
	urgent bool
	faint  bool // the title is faint
	text   string
}

const (
	amber  = "230;166;20"
	red    = "232;76;82"
	green  = "46;166;98"
	violet = "146;110;234"
	orange = "232;124;60"
	cyan   = "22;166;196"
	blue   = "64;140;232"
	gray   = "138;143;152"
)

var styles = map[string]style{
	NeedsInput: {"◆", amber, 0, true, false, "needs input"},
	NeedsYou:   {"◆", amber, 0, true, false, "needs you"},
	Failed:     {"✗", red, 1, false, false, "failed"},
	Ready:      {"✓", green, 2, false, false, "ready"},
	Done:       {"✓", green, 2, false, false, "done"},
	Merging:    {"⇡", violet, 3, false, false, "merging"},
	Fixing:     {"◑", orange, 3, false, false, "fixing"},
	Checking:   {"◐", cyan, 3, false, false, "checking"},
	Working:    {"●", blue, 3, false, false, "working"},
	Idle:       {"◇", gray, 4, false, true, "idle"},
	Waiting:    {"◌", gray, 5, false, true, "waiting"},
	Stopped:    {"■", gray, 5, false, true, "stopped"},
	Gone:       {"□", gray, 5, false, true, "gone"},
}

func styleOf(key string) style {
	if s, ok := styles[key]; ok {
		return s
	}
	s := styles[Idle]
	s.text = strings.ReplaceAll(key, "_", " ")
	return s
}

const (
	// Margin is what Claude Code takes from COLUMNS: it indents the status
	// line by two cells and cuts a longer row than COLUMNS-4 with "…"
	// (Claude Code 2.1.295, a statusLine without padding, as install writes
	// it).
	Margin    = 4
	columnGap = 3
	maxRows   = 4
	minLeader = 3  // dots after the longest title of a column
	minTitle  = 12 // titles shrink to this before the usage cell steps down
	minBadge  = 16 // an urgent badge is cut to this before the title gives more: "needs you · max…"
)

// columnsFrom are the terminal widths from which the ledger has 2, 3 and 4
// columns.
var columnsFrom = []int{124, 187, 250}

// Age is the time since since, at most five columns: now, 42m, 1h05m, 12h.
func Age(since, now time.Time) string {
	if since.IsZero() {
		return ""
	}
	m := int(now.Sub(since).Minutes())
	switch {
	case m < 1:
		return "now"
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m/60 >= 10:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

// shortReason is the start of a note, up to its first ": ": the part that
// names the problem.
func shortReason(note string, max int) string {
	note = strings.Join(strings.Fields(clean(note)), " ")
	if head, _, ok := strings.Cut(note, ": "); ok && head != "" {
		note = head
	}
	return truncate(note, max)
}

// A segment is text with its SGR parameters ("" for plain text).
type segment struct {
	text string
	sgr  string
}

type line []segment

func (l line) width() int {
	w := 0
	for _, s := range l {
		w += width(s.text)
	}
	return w
}

// clip cuts the line to max cells at a cell boundary.
func (l line) clip(max int) line {
	var out line
	used := 0
	for _, s := range l {
		if used >= max {
			break
		}
		if w := width(s.text); used+w <= max {
			out = append(out, s)
			used += w
			continue
		}
		var b strings.Builder
		for _, r := range s.text {
			cw := cellWidth(r)
			if used+cw > max {
				break
			}
			b.WriteRune(r)
			used += cw
		}
		out = append(out, segment{b.String(), s.sgr})
		break
	}
	return out
}

func (l line) render() string {
	var b strings.Builder
	for _, s := range l {
		if s.sgr == "" || s.text == "" {
			b.WriteString(s.text)
			continue
		}
		b.WriteString("\x1b[" + s.sgr + "m" + s.text + "\x1b[0m")
	}
	return b.String()
}

// painter makes segments, without colours under NO_COLOR (bold, faint and
// reverse video stay).
type painter struct{ noColor bool }

func (p painter) fg(rgb, text string) segment {
	if p.noColor {
		return segment{text, ""}
	}
	return segment{text, "38;2;" + rgb}
}

type entry struct {
	t     Task
	st    style
	title string
	state string
}

func newEntry(t Task, reasonMax int) entry {
	st := styleOf(t.State)
	state := t.Text
	if state == "" {
		state = st.text
	}
	state = clean(state)
	if t.State == NeedsYou && t.Note != "" && t.Text == "" {
		if r := shortReason(t.Note, reasonMax); r != "" {
			state += " · " + r
		}
	}
	return entry{t: t, st: st, title: clean(t.Title), state: state}
}

func (e entry) label() string { return "T" + strconv.Itoa(e.t.ID) }

// Layout lays out the ledger: one row per task, most urgent first, in one
// to four columns by width, at most four rows (the last one "+N more" when
// the tasks do not fit), no row wider than Claude Code shows (COLUMNS minus
// Margin). It is a pure function of its arguments.
func Layout(tasks []Task, columns int, now time.Time, noColor bool) []string {
	usable := columns - Margin
	if len(tasks) == 0 || usable < 1 {
		return nil
	}
	p := painter{noColor}
	ncol := 1
	for _, from := range columnsFrom {
		if columns >= from {
			ncol++
		}
	}
	cellMax := (usable - columnGap*(ncol-1)) / ncol
	reasonMax := min(24, max(8, cellMax/5))
	entries := make([]entry, len(tasks))
	for i, t := range tasks {
		entries[i] = newEntry(t, reasonMax)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].st.rank != entries[j].st.rank {
			return entries[i].st.rank < entries[j].st.rank
		}
		return entries[i].t.ID < entries[j].t.ID
	})
	shown, rest := entries, []entry(nil)
	if len(entries) > maxRows*ncol {
		shown, rest = entries[:(maxRows-1)*ncol], entries[(maxRows-1)*ncol:]
	}
	rows := (len(shown) + ncol - 1) / ncol
	var cols [][]line
	for i := 0; i < len(shown); i += rows {
		cols = append(cols, column(shown[i:min(i+rows, len(shown))], cellMax, now, p))
	}
	var lines []line
	for r := 0; r < rows; r++ {
		var l line
		for i, c := range cols {
			if r >= len(c) {
				continue
			}
			if i > 0 {
				l = append(l, segment{strings.Repeat(" ", columnGap), ""})
			}
			l = append(l, c[r]...)
		}
		lines = append(lines, l)
	}
	if len(rest) > 0 {
		lines = append(lines, append(line{{"  ", ""}}, overflow(rest, usable-2, p)...))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.clip(usable).render()
	}
	return out
}

// column lays out the cells of one column. Ids, states, usage and ages are
// aligned within it; the title area is as wide as the column's longest title
// plus a short leader, and no wider than the cell allows, so a short title in
// a wide terminal does not trail dozens of dots. An urgent badge runs across
// the usage cell. In a narrow cell titles shrink first, down to minTitle,
// then the usage cell steps down, then the badge text is cut, down to
// minBadge; then the title gives what is left and states are cut. The age
// is never cut while the cell has room for the row without title and state.
func column(entries []entry, cellMax int, now time.Time, p painter) []line {
	idW, stateW, urgentW, titleW, changeW, marks, usage := 0, 0, 0, 0, 0, false, false
	for _, e := range entries {
		idW = max(idW, width(e.label()))
		if e.st.urgent {
			urgentW = max(urgentW, width(e.state))
		} else {
			stateW = max(stateW, width(e.state))
		}
		titleW = max(titleW, width(e.title))
		marks = marks || e.t.Mark != ""
		usage = usage || showsUsage(e)
		changeW = max(changeW, width(changePart(e, now, p).text))
	}
	base := 4 + idW + 1 + 2 + 1 + 5 // rail glyph id … state age
	if marks {
		base += 2
	}
	want := min(minTitle, titleW+1+minLeader) // the title area kept while the rest gives
	// The usage cell's widths, widest first: the total and the change, then
	// the total alone.
	widths := []int{totalCells}
	if changeW > 0 {
		widths = []int{totalCells + 2 + changeW, totalCells}
	}
	usageW, span := 0, 0
	if usage {
		for _, w := range widths {
			if cellMax-(base+max(stateW, urgentW-w-1)+w+1) >= want {
				usageW, span = w, w+1
				break
			}
		}
	}
	badgeW := urgentW
	if over := base + max(stateW, badgeW-span) + span + want - cellMax; over > 0 {
		badgeW = max(stateW+span, badgeW-over, min(badgeW, minBadge))
	}
	stateW = min(max(stateW, badgeW-span), max(0, cellMax-base-span))
	badgeW = min(badgeW, stateW+span)
	fixed := base + stateW + span
	area := max(0, min(cellMax-fixed, titleW+1+minLeader))
	out := make([]line, len(entries))
	for i, e := range entries {
		st := e.st
		var title string
		if w := width(e.title); w+2 <= area {
			title = e.title + " " + strings.Repeat("·", area-w-1)
		} else {
			title = pad(truncate(e.title, area), area)
		}
		rail := "▎"
		if st.urgent && p.noColor {
			rail = "▌"
		}
		text := truncate(e.state, stateW)
		if st.urgent {
			text = truncate(e.state, badgeW)
		}
		state := p.fg(st.rgb, text)
		if st.urgent {
			state.sgr = "1;38;2;20;20;20;48;2;" + st.rgb
			if p.noColor {
				state.sgr = "1;7"
			}
		}
		titleSgr := ""
		if st.faint {
			titleSgr = "2"
		}
		l := line{p.fg(st.rgb, rail), {" ", ""}, p.fg(st.rgb, st.glyph), {" ", ""}, {pad(e.label(), idW), "1"}, {" ", ""}}
		if marks {
			mark := e.t.Mark
			if mark == "" {
				mark = " "
			}
			l = append(l, segment{mark, ""}, segment{" ", ""})
		}
		l = append(l, segment{title, titleSgr}, segment{"  ", ""}, state)
		if cell := usageCell(e, usageW, now, p); cell != nil {
			l = append(l, segment{strings.Repeat(" ", stateW-width(text)+1), ""})
			l = append(l, cell...)
			l = append(l, segment{" ", ""})
		} else {
			l = append(l, segment{strings.Repeat(" ", stateW+span-width(text)+1), ""})
		}
		l = append(l, segment{padLeft(Age(e.t.Since, now), 5), "2"})
		out[i] = l
	}
	return out
}

// overflow is "+7 more" followed by the hidden tasks: by id when they fit,
// else as counts per state, else the bare count.
func overflow(rest []entry, max int, p painter) line {
	head := line{{"+" + strconv.Itoa(len(rest)), "1"}, {" more  ", "2"}}
	var byID line
	for i, e := range rest {
		if i > 0 {
			byID = append(byID, segment{"  ", ""})
		}
		byID = append(byID, p.fg(e.st.rgb, e.st.glyph), segment{" " + e.label(), ""})
	}
	if head.width()+byID.width() <= max {
		return append(head, byID...)
	}
	type group struct {
		st style
		n  int
	}
	var groups []group
	for _, e := range rest {
		found := false
		for i := range groups {
			if groups[i].st.glyph == e.st.glyph && groups[i].st.rgb == e.st.rgb {
				groups[i].n++
				found = true
			}
		}
		if !found {
			groups = append(groups, group{e.st, 1})
		}
	}
	var tally line
	for i, g := range groups {
		if i > 0 {
			tally = append(tally, segment{" ", ""})
		}
		tally = append(tally, p.fg(g.st.rgb, g.st.glyph), segment{strconv.Itoa(g.n), ""})
	}
	if head.width()+tally.width() <= max {
		return append(head, tally...)
	}
	return line{{truncate("+"+strconv.Itoa(len(rest))+" more", max), "1"}}
}
