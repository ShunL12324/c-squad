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
	Margin      = 4
	columnGap   = 3
	fieldGap    = 2
	maxRows     = 4
	maxCols     = 4
	minLeader   = 3  // a title this many cells shorter than the column's title field gets a dotted leader
	minColTitle = 16 // a column is added only while every title keeps this many cells
	minTitle    = 12 // titles shrink to this before the usage cell steps down
	minBadge    = 16 // an urgent badge is cut to this before the title gives more: "needs you · max…"
	reasonMax   = 24 // the reason of a needs-you badge
)

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

func newEntry(t Task) entry {
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

// Layout lays out the ledger: one row per task, most urgent first, at most
// four rows (the last one "+N more" when the tasks do not fit), no row wider
// than Claude Code shows (COLUMNS minus Margin). Up to four tasks take one
// column; more take ⌈n/4⌉ columns when they fit with every title keeping
// minColTitle cells, else one column fewer. Each column is as wide as its
// widest row. It is a pure function of its arguments.
func Layout(tasks []Task, columns int, now time.Time, noColor bool) []string {
	usable := columns - Margin
	if len(tasks) == 0 || usable < 1 {
		return nil
	}
	p := painter{noColor}
	entries := make([]entry, len(tasks))
	for i, t := range tasks {
		entries[i] = newEntry(t)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].st.rank != entries[j].st.rank {
			return entries[i].st.rank < entries[j].st.rank
		}
		return entries[i].t.ID < entries[j].t.ID
	})
	n := len(entries)
	for ncol := min(maxCols, (n+maxRows-1)/maxRows); ncol >= 1; ncol-- {
		shown, rest := entries, []entry(nil)
		if n > maxRows*ncol {
			shown, rest = entries[:(maxRows-1)*ncol], entries[(maxRows-1)*ncol:]
		}
		rows := (len(shown) + ncol - 1) / ncol
		var cols [][]entry
		for i := 0; i < len(shown); i += rows {
			cols = append(cols, shown[i:min(i+rows, len(shown))])
		}
		try := func(f fit) ([]fields, bool) {
			ws := make([]fields, len(cols))
			total := columnGap * (len(cols) - 1)
			for i, c := range cols {
				ws[i] = measure(c, f, now, p)
				total += ws[i].row()
			}
			return ws, total <= usable
		}
		steps := fitSteps(shown, ncol > 1)
		for k, f := range steps {
			ws, ok := try(f)
			if !ok && !(ncol == 1 && k == len(steps)-1) {
				continue
			}
			// A badge wider than the state and usage fields leaves room
			// for usage parts that the step down dropped.
			for u := 2; ok && u > f.usage; u-- {
				g := f
				g.usage = u
				if gws, gok := try(g); gok {
					ws, f = gws, g
					break
				}
			}
			return render(cols, ws, rest, f, usable, now, p)
		}
	}
	return nil
}

// fit is how much of the rows a layout keeps: the widest title, the usage
// cell (2: the total and the change, 1: the total, 0: none), the widest
// urgent badge and the widest state.
type fit struct{ title, usage, badge, state int }

const whole = 1 << 20 // a fit field that cuts nothing

// fitSteps are the fits to try, roomiest first. A layout of several columns
// shortens titles, down to minColTitle, then cuts the badge reason, down to
// minBadge. One column steps down: titles shrink to minTitle, then the
// change goes, then the total, then the badge reason is cut down to
// minBadge, then the title gives the rest, then states are cut. The age is
// never cut.
func fitSteps(es []entry, multi bool) []fit {
	titleW, stateW := 0, 0
	for _, e := range es {
		titleW = max(titleW, width(e.title))
		stateW = max(stateW, width(e.state))
	}
	var steps []fit
	if multi {
		keep := min(minColTitle, titleW)
		for t := titleW; t >= keep; t-- {
			steps = append(steps, fit{t, 2, whole, whole})
		}
		for b := stateW - 1; b >= minBadge; b-- {
			steps = append(steps, fit{keep, 2, b, whole})
		}
		return steps
	}
	keep := min(minTitle, titleW)
	for u := 2; u >= 0; u-- {
		for t := titleW; t >= keep; t-- {
			steps = append(steps, fit{t, u, whole, whole})
		}
	}
	for b := stateW - 1; b >= minBadge; b-- {
		steps = append(steps, fit{keep, 0, b, whole})
	}
	for t := keep; t >= 0; t-- {
		steps = append(steps, fit{t, 0, minBadge, whole})
	}
	for s := stateW; s >= 0; s-- {
		steps = append(steps, fit{0, 0, min(minBadge, s), s})
	}
	return steps
}

// fields are the widths of the aligned fields of a column, each as wide as
// its widest value. block is the state and the usage cell, or an urgent
// badge across both: a badge wider than both widens the block, after the
// usage cell, so the age stays in its column.
type fields struct {
	id, title, state, total, change, age, block int
	marks                                       bool
}

// usage is the width of the usage cell: the total, then a space and the
// change part.
func (w fields) usage() int {
	if w.total == 0 {
		return 0
	}
	if w.change > 0 {
		return w.total + 1 + w.change
	}
	return w.total
}

// span is the usage cell with the gap before it.
func (w fields) span() int {
	if u := w.usage(); u > 0 {
		return fieldGap + u
	}
	return 0
}

// row is the width of each row of the column.
func (w fields) row() int {
	n := 4 + w.id + 1 + w.title + fieldGap + w.block // rail glyph id title state
	if w.marks {
		n += 2
	}
	if w.age > 0 {
		n += fieldGap + w.age
	}
	return n
}

func measure(es []entry, f fit, now time.Time, p painter) fields {
	var w fields
	badge := 0
	for _, e := range es {
		w.id = max(w.id, width(e.label()))
		w.marks = w.marks || e.t.Mark != ""
		w.title = max(w.title, min(f.title, width(e.title)))
		if e.st.urgent {
			badge = max(badge, width(truncate(e.state, f.badge)))
		} else {
			w.state = max(w.state, width(truncate(e.state, f.state)))
		}
		if f.usage >= 1 {
			w.total = max(w.total, width(totalPart(e).text))
		}
		if f.usage >= 2 && showsChange(e) {
			w.change = max(w.change, changeCells, width(changePart(e, now, p).text))
		}
		w.age = max(w.age, width(Age(e.t.Since, now)))
	}
	w.block = max(w.state+w.span(), badge)
	return w
}

func render(cols [][]entry, ws []fields, rest []entry, f fit, usable int, now time.Time, p painter) []string {
	cells := make([][]line, len(cols))
	for i, c := range cols {
		cells[i] = make([]line, len(c))
		for r, e := range c {
			cells[i][r] = cell(e, ws[i], f, now, p)
		}
	}
	var lines []line
	for r := range cells[0] {
		var l line
		for i, c := range cells {
			if r >= len(c) {
				continue
			}
			if i > 0 {
				l = append(l, spaces(columnGap+ws[i-1].row()-cells[i-1][r].width()))
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

func spaces(n int) segment { return segment{strings.Repeat(" ", max(0, n)), ""} }

// cell is the row of one task in its column: rail, glyph, id, engine mark,
// the title (with a dotted leader when it is minLeader or more cells
// shorter than the title field), the state, the usage cell and the age,
// each in its field, two cells apart. An urgent badge runs across the usage
// cell.
func cell(e entry, w fields, f fit, now time.Time, p painter) line {
	st := e.st
	rail := "▎"
	if st.urgent && p.noColor {
		rail = "▌"
	}
	l := line{p.fg(st.rgb, rail), {" ", ""}, p.fg(st.rgb, st.glyph), {" ", ""}, {pad(e.label(), w.id), "1"}, {" ", ""}}
	if w.marks {
		mark := e.t.Mark
		if mark == "" {
			mark = " "
		}
		l = append(l, segment{mark, ""}, segment{" ", ""})
	}
	title := truncate(e.title, w.title)
	if gap := w.title - width(title); gap >= minLeader {
		title += " " + strings.Repeat("·", gap-1)
	} else {
		title = pad(title, w.title)
	}
	titleSgr := ""
	if st.faint {
		titleSgr = "2"
	}
	l = append(l, segment{title, titleSgr}, segment{"  ", ""})
	if st.urgent {
		badge := truncate(e.state, f.badge)
		sgr := "1;38;2;20;20;20;48;2;" + st.rgb
		if p.noColor {
			sgr = "1;7"
		}
		l = append(l, segment{badge, sgr}, spaces(w.block-width(badge)))
	} else {
		text := truncate(e.state, f.state)
		l = append(l, p.fg(st.rgb, text), spaces(w.state-width(text)))
		if w.span() > 0 {
			l = append(append(l, segment{"  ", ""}), usageCell(e, w, f.usage >= 2, now, p)...)
		}
		l = append(l, spaces(w.block-w.state-w.span()))
	}
	if w.age > 0 {
		l = append(l, segment{"  ", ""}, segment{padLeft(Age(e.t.Since, now), w.age), "2"})
	}
	return l
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
