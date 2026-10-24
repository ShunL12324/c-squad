package statusline

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func T(id int, title, state string, minutesAgo float64) Task {
	return Task{ID: id, Title: title, State: state, Since: now.Add(-time.Duration(minutesAgo * float64(time.Minute)))}
}

// twelve are the tasks of the #44 samples (1.x states plus 3.0's).
func twelve() []Task {
	return []Task{
		T(2, "修复 Windows 下 shim 测试不稳定", NeedsInput, 18),
		{ID: 9, Title: "Install migration from 1.x", State: NeedsYou, Note: "merge conflict: src/a.go, src/b.go", Since: now.Add(-95 * time.Minute)},
		T(10, "Peek transcript paging", Failed, 130),
		T(3, "Add --json to csquad status", Done, 65),
		T(1, "Port the store to modernc sqlite", Working, 42),
		T(4, "Codex app-server reconnect", Working, 7),
		{ID: 6, Title: "Retry SQLITE_BUSY in Transition", State: Checking, Text: "checking 1/3", Since: now.Add(-12 * time.Minute)},
		{ID: 7, Title: "Windows PATH resolution", State: Fixing, Text: "fixing 2/3", Since: now.Add(-31 * time.Minute)},
		T(8, "Status line chip grid", Merging, 3),
		T(12, "重构状态行的宽度计算", Idle, 5),
		{ID: 5, Title: "Document hooks in README", State: Waiting, Text: "waiting T4", Since: now.Add(-7 * time.Minute)},
		T(11, "Benchmark AdvanceAll", Stopped, 240),
	}
}

// scenario are the 1, 3, 5 and 9 tasks of the layout options page: engine
// marks, usage cells, a stall, a gateway that reports no usage, a CJK title
// and a needs-you badge wider than the state and usage fields.
func scenario(n int) []Task {
	ago := func(m float64) time.Time { return now.Add(-time.Duration(m * float64(time.Minute))) }
	t2 := Task{ID: 2, Mark: "✻", Title: "修复 Windows 下 shim 测试不稳定", State: NeedsInput, Since: ago(18), Usage: usage(90_000, 0, 1000)}
	t9 := Task{ID: 9, Mark: "✻", Title: "Install migration from 1.x", State: NeedsYou, Note: "merge conflict: src/a.go, src/b.go", Since: ago(95), Usage: usage(400_000, 0)}
	t10 := Task{ID: 10, Mark: "✻", Title: "Peek transcript paging", State: Failed, Since: ago(130), Usage: usage(61_000, 0)}
	t4 := Task{ID: 4, Mark: "✻", Title: "readme demo recording", State: Done, Since: ago(3), Usage: usage(290_000, 0)}
	t1 := Task{ID: 1, Mark: "⬡", Title: "Codex app-server reconnect", State: Working, Since: ago(7), Usage: usage(326_000, 0, 2000, 4100)}
	t6 := Task{ID: 6, Mark: "⬡", Title: "Retry SQLITE_BUSY in Transition", State: Checking, Text: "checking 1/3", Since: ago(12), Usage: usage(88_000, 0, 1000, 0)}
	t7 := Task{ID: 7, Mark: "✻", Title: "Windows PATH resolution", State: Fixing, Text: "fixing 2/3", Since: ago(31), Usage: usage(210_000, 3*time.Minute, 0, 0, 0)}
	t8 := Task{ID: 8, Mark: "✻", Title: "Status line chip grid", State: Merging, Since: ago(3), Usage: usage(0, 0, -1, -1)}
	t5 := Task{ID: 5, Mark: "✻", Title: "Document hooks in README", State: Waiting, Text: "waiting T1", Since: ago(7)}
	switch n {
	case 1:
		return []Task{t4}
	case 3:
		return []Task{t9, t1, t4}
	case 5:
		return []Task{t2, t9, t1, t7, t5}
	}
	return []Task{t2, t9, t10, t4, t1, t6, t7, t8, t5}
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func strip(s string) string { return sgr.ReplaceAllString(s, "") }

// columnSep is the gap before the second and later ledger columns of a row.
var columnSep = regexp.MustCompile(`   +[▎▌] `)

func ledgerColumns(rows []string) int {
	return len(columnSep.FindAllString(strip(rows[0]), -1)) + 1
}

// cellEnds are the cells at which the ledger columns of a row end.
func cellEnds(row string) []int {
	plain := strip(row)
	var ends []int
	for _, m := range columnSep.FindAllStringIndex(plain, -1) {
		ends = append(ends, width(plain[:m[0]]))
	}
	return append(ends, width(plain))
}

// checkGolden compares got with testdata/name; -update rewrites the file.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	checkFile(t, filepath.Join("testdata", name), got)
}

// checkFile compares got with file; -update rewrites it.
func checkFile(t *testing.T, file, got string) {
	t.Helper()
	if *update {
		os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs (go test -run %s -update rewrites it):\n%s", file, t.Name(), got)
	}
}

// goldenText is the ledger of tasks at each width, a "=== label" line
// before each.
func goldenText(b *strings.Builder, label string, tasks []Task, columns int, noColor bool) {
	fmt.Fprintf(b, "=== %s\n", label)
	for _, l := range Layout(tasks, columns, now, noColor) {
		b.WriteString(strings.ReplaceAll(l, "\x1b", `\e`) + "\n")
	}
}

func goldenName(base string, noColor bool) string {
	if noColor {
		return base + "-nocolor.golden"
	}
	return base + "-color.golden"
}

func TestAge(t *testing.T) {
	for _, c := range []struct {
		min  float64
		want string
	}{{0.5, "now"}, {42, "42m"}, {65, "1h05m"}, {599, "9h59m"}, {600, "10h"}, {2000, "33h"}} {
		if got := Age(now.Add(-time.Duration(c.min*float64(time.Minute))), now); got != c.want {
			t.Errorf("Age(%v min) = %q, want %q", c.min, got, c.want)
		}
	}
	if Age(time.Time{}, now) != "" {
		t.Error("zero time must have no age")
	}
}

// TestGolden pins the layout at 80/120/160/255 columns x 0/1/3/6/12 tasks x
// colour/NO_COLOR. Run with -update to rewrite testdata after a deliberate
// change, then read the diff.
func TestGolden(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		var b strings.Builder
		for _, n := range []int{0, 1, 3, 6, 12} {
			for _, c := range []int{80, 120, 160, 255} {
				goldenText(&b, fmt.Sprintf("%d tasks, %d columns", n, c), twelve()[:n], c, noColor)
			}
		}
		checkGolden(t, goldenName("ledger", noColor), b.String())
	}
}

// TestGoldenScenarios pins the 1, 3, 5 and 9 tasks of the layout options
// page at 80/124/160/250 columns (-update rewrites it).
func TestGoldenScenarios(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		var b strings.Builder
		for _, n := range []int{1, 3, 5, 9} {
			for _, c := range []int{80, 124, 160, 250} {
				goldenText(&b, fmt.Sprintf("%d tasks, %d columns", n, c), scenario(n), c, noColor)
			}
		}
		checkGolden(t, goldenName("ledger-scenarios", noColor), b.String())
	}
}

func TestScenarios(t *testing.T) {
	for _, c := range []int{80, 124, 160, 250} {
		one := Layout(scenario(1), c, now, true)
		if len(one) != 1 || strip(one[0]) != "▎ ✓ T4 ✻ readme demo recording  done  290k  3m" {
			t.Errorf("1 task, %d columns: %q", c, one)
		}
		if three := Layout(scenario(3), c, now, true); len(three) != 3 || ledgerColumns(three) != 1 {
			t.Errorf("3 tasks, %d columns: want 3 rows in one column, got %q", c, three)
		}
	}
	for _, want := range []struct {
		n, columns, ncol, rows int
		more                   string
	}{
		{5, 80, 1, 4, "+2 more"},
		{5, 124, 2, 3, ""},
		{5, 160, 2, 3, ""},
		{5, 250, 2, 3, ""},
		{9, 80, 1, 4, "+6 more"},
		{9, 124, 2, 4, "+3 more"},
		{9, 160, 2, 4, "+3 more"},
		{9, 250, 3, 3, ""},
	} {
		rows := Layout(scenario(want.n), want.columns, now, true)
		last := strip(rows[len(rows)-1])
		if len(rows) != want.rows || ledgerColumns(rows) != want.ncol || want.more != "" && !strings.HasPrefix(last, "  "+want.more) || want.more == "" && strings.Contains(last, "more") {
			t.Errorf("%d tasks, %d columns: want %d rows in %d columns %s, got\n%s", want.n, want.columns, want.rows, want.ncol, want.more, strip(strings.Join(rows, "\n")))
		}
	}
}

// Up to four tasks take one column at any width; more take ⌈n/4⌉ columns
// only while every title keeps minColTitle cells. Columns are as wide as
// their widest row.
func TestColumnRule(t *testing.T) {
	title := regexp.MustCompile(`T\d+ +(?:[✻⬡ ] )?(.*?)  `)
	sixteen := append(twelve(), T(13, "Extra task one", Idle, 1), T(14, "Extra task two", Idle, 1), T(15, "Extra task three", Idle, 1), T(16, "Extra task four", Idle, 1))
	for _, tasks := range [][]Task{twelve(), usageTasks(), sixteen, scenario(5), scenario(9)} {
		last := 0
		for c := 40; c <= 300; c++ {
			for n := 1; n <= 4; n++ {
				if rows := Layout(tasks[:n], c, now, true); ledgerColumns(rows) != 1 {
					t.Fatalf("%d tasks at %d columns: %d ledger columns\n%s", n, c, ledgerColumns(rows), strip(strings.Join(rows, "\n")))
				}
			}
			rows := Layout(tasks, c, now, true)
			ncol := ledgerColumns(rows)
			if ncol < last || ncol > (len(tasks)+3)/4 {
				t.Fatalf("%d tasks at %d columns: %d ledger columns (%d one column narrower)", len(tasks), c, ncol, last)
			}
			last = ncol
			if ncol == 1 {
				continue
			}
			for _, r := range rows {
				for _, m := range title.FindAllStringSubmatch(strip(r), -1) {
					// A wide character does not straddle the field's edge.
					if tt := strings.TrimRight(m[1], " ·"); strings.HasSuffix(tt, "…") && width(tt)+1 < minColTitle {
						t.Fatalf("%d columns: a title cut to %d cells in %d ledger columns: %q", c, width(tt), ncol, strip(r))
					}
				}
			}
		}
		if last != (len(tasks)+3)/4 {
			t.Errorf("%d tasks at 300 columns: %d ledger columns", len(tasks), last)
		}
	}
	// Content-sized: five tasks in a wide terminal leave the rest empty.
	rows := Layout(scenario(5), 300, now, true)
	if w := width(strip(rows[0])); ledgerColumns(rows) != 2 || w > 160 {
		t.Errorf("5 tasks at 300 columns: %d columns, %d cells wide", ledgerColumns(rows), w)
	}
}

func TestOrderAndOverflow(t *testing.T) {
	tasks := twelve()[:6]
	// Input order must not matter.
	for i, j := 0, len(tasks)-1; i < j; i, j = i+1, j-1 {
		tasks[i], tasks[j] = tasks[j], tasks[i]
	}
	rows := Layout(tasks, 80, now, true)
	if len(rows) != 4 {
		t.Fatalf("want 3 rows and +3 more, got %q", rows)
	}
	for i, re := range []string{`^▌ ◆ T2 .*needs input\s+18m$`, `^▌ ◆ T9 .*needs you · merge conflict\s+1h35m$`, `^▎ ✗ T10 .*failed\s+2h10m$`} {
		if !regexp.MustCompile(re).MatchString(strip(rows[i])) {
			t.Errorf("row %d = %q, want %s", i, strip(rows[i]), re)
		}
	}
	if got := strip(rows[3]); got != "  +3 more  ✓ T3  ● T1  ● T4" {
		t.Errorf("overflow = %q", got)
	}
	// Narrow: counts instead of ids.
	if got := strip(Layout(twelve(), 30, now, true)[3]); got != "  +9 more  ✓1 ●3 ◐1 ◑1 ⇡1 ◇1 ◌1 ■1" && !strings.HasPrefix(got, "  +9 more") {
		t.Errorf("narrow overflow = %q", got)
	}
	// Column by column: the second column starts after the first one's rows.
	two := Layout(twelve()[:6], 140, now, true)
	if len(two) != 3 || !regexp.MustCompile(`T2 .*T3 `).MatchString(strip(two[0])) {
		t.Errorf("two columns: %q", two)
	}
}

func TestEveryTaskNamedAtEverySize(t *testing.T) {
	ids := regexp.MustCompile(`\bT\d+\b`)
	for _, n := range []int{0, 1, 3, 6, 12} {
		for _, c := range []int{80, 120, 160, 255} {
			for _, noColor := range []bool{true, false} {
				rows := Layout(twelve()[:n], c, now, noColor)
				if n == 0 && rows != nil {
					t.Errorf("no tasks must print nothing, got %q", rows)
				}
				if len(rows) > 4 {
					t.Errorf("%d tasks at %d: %d rows", n, c, len(rows))
				}
				seen := map[string]bool{}
				for _, id := range ids.FindAllString(strip(strings.Join(rows, "\n")), -1) {
					seen[id] = true
				}
				if len(seen) != n {
					t.Errorf("%d tasks at %d columns: %d named\n%s", n, c, len(seen), strip(strings.Join(rows, "\n")))
				}
			}
		}
	}
}

// Claude Code shows COLUMNS-4 cells of a status line row and cuts the rest
// with "…".
func TestFitsWhatClaudeCodeShows(t *testing.T) {
	long := append(twelve(), T(13, strings.Repeat("很长的标题", 30), Working, 1))
	for c := 1; c <= 300; c++ {
		for _, tasks := range [][]Task{twelve(), twelve()[:1], twelve()[:3], long, longBadge()} {
			for _, noColor := range []bool{true, false} {
				for _, r := range Layout(tasks, c, now, noColor) {
					if w := width(strip(r)); w > c-4 { // observed on Claude Code 2.1.295
						t.Fatalf("width %d: row of %d cells: %q", c, w, strip(r))
					}
				}
			}
		}
	}
}

// longBadge are seven tasks with a needs-you badge, usage cells and engine
// marks: at 140 columns the ages of the 3.0.0 ledger were cut.
func longBadge() []Task {
	tasks := usageTasks()[:7]
	tasks[1].Note = "max rounds: the check failed in 3 rounds"
	return tasks
}

// wideTasks have long titles, a long needs-input text and a long waiting
// text.
func wideTasks() []Task {
	wide := longBadge()
	for i := range wide {
		wide[i].ID += 100
		wide[i].Title = strings.Repeat("Long title ", 8)
	}
	wide[0].Text = "needs input · a question about the release notes"
	wide[2].State, wide[2].Text = Waiting, "waiting T101,T102,T103"
	return wide
}

// The age is never cut: the title shrinks first, then the usage cell steps
// down and the badge text is cut, then the title gives the rest.
func TestAgeNeverCut(t *testing.T) {
	id := regexp.MustCompile(`T(\d+)`)
	for c := 40; c <= 300; c++ {
		for _, tasks := range [][]Task{longBadge(), wideTasks(), usageTasks(), twelve()} {
			ages := map[string]string{}
			for _, tk := range tasks {
				ages["T"+strconv.Itoa(tk.ID)] = Age(tk.Since, now)
			}
			rows := Layout(tasks, c, now, true)
			for _, r := range rows {
				plain := strip(r)
				if strings.HasPrefix(plain, "  +") {
					continue // the overflow row
				}
				for _, cell := range columnSep.Split(plain, -1) {
					m := id.FindString(cell)
					if !strings.HasSuffix(cell, " "+ages[m]) {
						t.Fatalf("%d columns: the age of %s (%s) is cut: %q", c, m, ages[m], plain)
					}
				}
			}
		}
	}
}

// The rows of a ledger column end at the same cell: the age is right-
// aligned in its field on every row, urgent ones included, at every width.
func TestAgesAligned(t *testing.T) {
	sets := [][]Task{longBadge(), wideTasks(), usageTasks(), twelve(), scenario(1), scenario(3), scenario(5), scenario(9)}
	for c := 40; c <= 300; c++ {
		for _, tasks := range sets {
			var first []int
			rows := Layout(tasks, c, now, true)
			for _, r := range rows {
				if strings.HasPrefix(strip(r), "  +") {
					continue
				}
				ends := cellEnds(r)
				if first == nil {
					first = ends
				}
				for k := range min(len(ends), len(first)) {
					if ends[k] != first[k] {
						t.Fatalf("%d columns: ledger column %d ends at %d and %d:\n%s", c, k+1, first[k], ends[k], strip(strings.Join(rows, "\n")))
					}
				}
			}
		}
	}
}

// A badge wider than the state and usage fields runs across the usage cell
// and on into the room before the age, which stays in its column; only
// when the row does not fit is the badge reason cut, down to minBadge.
func TestLongBadge(t *testing.T) {
	tasks := scenario(3)
	for _, c := range []int{80, 124, 250} {
		rows := Layout(tasks, c, now, true)
		want := []string{
			"▌ ◆ T9 ✻ Install migration from 1.x  needs you · merge conflict  1h35m",
			"▎ ✓ T4 ✻ readme demo recording ····  done     290k                  3m",
			"▎ ● T1 ⬡ Codex app-server reconnect  working  326k +4.1k            7m",
		}
		for i, r := range rows {
			if strip(r) != want[i] {
				t.Errorf("%d columns, row %d:\n got %q\nwant %q", c, i, strip(r), want[i])
			}
		}
	}
	// Narrow: the title shrinks, the usage steps down, then the reason is
	// cut; the age stays.
	var badges []string
	for c := 70; c >= 40; c-- {
		row := strip(Layout(tasks, c, now, true)[0])
		m := regexp.MustCompile(`(needs you.*?)  +1h35m$`).FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("%d columns: %q", c, row)
		}
		if w := width(m[1]); w < minBadge && c >= 40 {
			t.Errorf("%d columns: badge cut to %d cells: %q", c, w, row)
		}
		badges = append(badges, m[1])
	}
	if badges[0] != "needs you · merge conflict" || badges[len(badges)-1] != "needs you · mer…" {
		t.Errorf("badges from 70 to 40 columns: %q", badges)
	}
	// A cut badge is still wider than the state and usage fields: the usage
	// cell, which costs no room then, stays.
	if row := strip(Layout(tasks, 58, now, true)[2]); !strings.Contains(row, "working  326k +4.1k") {
		t.Errorf("58 columns: %q", row)
	}
}

// A stall shows idle 3m where the change was: the change part keeps
// changeCells cells, so the row and the rows next to it do not move
// between refreshes.
func TestStallKeepsTheRow(t *testing.T) {
	ago := func(m float64) time.Time { return now.Add(-time.Duration(m * float64(time.Minute))) }
	working := Task{ID: 1, Mark: "⬡", Title: "Codex app-server reconnect", State: Working, Since: ago(7), Usage: usage(326_000, 0, 2000, 4100)}
	refreshes := []*Usage{
		usage(210_000, 30*time.Second),        // one reading: no change yet
		usage(210_000, 0, 0, 1200),            // +1.2k
		usage(210_000, 0, 100, 0),             // +0
		usage(210_000, 0, 0, -1),              // +?
		usage(210_000, 3*time.Minute, 0, 0),   // idle 3m
		usage(210_000, 0, 20_000, 326_000),    // +326k
		usage(210_000, 9*time.Minute, 0, 0),   // idle 9m
		usage(210_000, 0, 9_000, 9_900_000-1), // +9.9M
	}
	for _, c := range []int{80, 124, 160} {
		var want []string
		for i, u := range refreshes {
			fixing := Task{ID: 7, Mark: "✻", Title: "Windows PATH resolution", State: Fixing, Text: "fixing 2/3", Since: ago(31), Usage: u}
			rows := Layout([]Task{working, fixing}, c, now, true)
			plain := []string{strip(rows[0]), strip(rows[1])}
			if i == 0 {
				want = plain
			}
			if plain[0] != want[0] || width(plain[1]) != width(want[1]) || u.Known && strings.Index(plain[1], "210k") != strings.Index(want[1], "210k") {
				t.Errorf("%d columns, refresh %d moved the rows:\n%s\n%s\nwas\n%s\n%s", c, i, plain[0], plain[1], want[0], want[1])
			}
		}
	}
	// Finished rows reserve no change part of their own.
	done := []Task{{ID: 4, Title: "readme demo recording", State: Done, Since: now, Usage: usage(290_000, 0)}}
	if got := strip(Layout(done, 80, now, true)[0]); got != "▎ ✓ T4 readme demo recording  done  290k  now" {
		t.Errorf("done row: %q", got)
	}
}

// TestGoldenLongBadge pins the ledger of longBadge at widths from one column
// to three (-update rewrites it).
func TestGoldenLongBadge(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		var b strings.Builder
		for _, c := range []int{80, 123, 124, 140, 186, 187, 249, 250} {
			goldenText(&b, fmt.Sprintf("%d columns", c), longBadge(), c, noColor)
		}
		checkGolden(t, goldenName("ledger-badge", noColor), b.String())
	}
}

// A title minLeader or more cells shorter than the column's title field gets
// a dotted leader up to the state, so the eye finds its state; the longest
// title and one a cell or two shorter get none. No row trails more dots than
// its title is short, at any width (the 1.x port filled a wide column with
// dots).
func TestLeader(t *testing.T) {
	tasks := []Task{T(1, "fix login", Working, 3), T(2, "add parser tests for empty input", NeedsInput, 18), T(10, "docs", Done, 65), T(11, "add parser tests for empty inp", Idle, 1)}
	for _, c := range []int{80, 120, 200, 300} {
		rows := Layout(tasks, c, now, true)
		want := []string{
			"▌ ◆ T2  add parser tests for empty input  needs input    18m",
			"▎ ✓ T10 docs ···························  done         1h05m",
			"▎ ● T1  fix login ······················  working         3m",
			"▎ ◇ T11 add parser tests for empty inp    idle            1m",
		}
		for i, r := range rows {
			if strip(r) != want[i] {
				t.Errorf("%d columns, row %d:\n got %q\nwant %q", c, i, strip(r), want[i])
			}
		}
	}
}

func TestNoColorTitlesAndUnknownStates(t *testing.T) {
	tasks := []Task{T(1, "tab\there\x1b[31m red", NeedsInput, 1), T(2, "写测试", "mystery", 1), T(3, "ok", Idle, 1)}
	plain := Layout(tasks, 100, now, true)
	for _, r := range plain {
		if regexp.MustCompile(`\x1b\[[34]8;`).MatchString(r) {
			t.Errorf("NO_COLOR row has colours: %q", r)
		}
		if strings.Contains(r, "\x1b[31m") {
			t.Errorf("control characters of a title reached the output: %q", r)
		}
	}
	if !strings.Contains(plain[0], "\x1b[1;7m") {
		t.Errorf("NO_COLOR: urgent state must be reverse video: %q", plain[0])
	}
	if !regexp.MustCompile(`^▌ ◆ T1 tab here \[31m red  needs input`).MatchString(strip(plain[0])) {
		t.Errorf("row 0 = %q", strip(plain[0]))
	}
	if !regexp.MustCompile(`◇ T2 写测试 ·+  mystery`).MatchString(strip(plain[1])) {
		t.Errorf("unknown state ranks and looks like idle: %q", strip(plain[1]))
	}
	colored := Layout(tasks, 100, now, false)
	if !strings.Contains(colored[0], "48;2;230;166;20") {
		t.Errorf("urgent state must be a filled badge: %q", colored[0])
	}
	if strip(colored[0]) != strings.Replace(strip(plain[0]), "▌", "▎", 1) {
		t.Errorf("colour changes the layout:\n%q\n%q", strip(colored[0]), strip(plain[0]))
	}
}

// A CJK character takes two cells: a CJK title and an ASCII title of the
// same width take the same title field, and states start in one column.
func TestCJKTitle(t *testing.T) {
	tasks := []Task{T(1, "重构状态行的宽度计算", Working, 1), T(2, "Refactor the widths.", Idle, 2), T(3, "状态行", Done, 3)}
	rows := Layout(tasks, 80, now, true)
	want := []string{
		"▎ ✓ T3 状态行 ·············  done     3m",
		"▎ ● T1 重构状态行的宽度计算  working  1m",
		"▎ ◇ T2 Refactor the widths.  idle     2m",
	}
	for i, r := range rows {
		if strip(r) != want[i] {
			t.Errorf("row %d:\n got %q\nwant %q", i, strip(r), want[i])
		}
	}
}

func TestCJKTruncation(t *testing.T) {
	tasks := []Task{T(1, strings.Repeat("宽", 80), Working, 1), T(2, "x", Idle, 1)}
	for c := 60; c <= 130; c++ {
		for _, r := range Layout(tasks, c, now, true) {
			plain := strip(r)
			if strings.Contains(plain, "宽") && !strings.Contains(plain, "…") {
				t.Errorf("%d: long CJK title not cut with an ellipsis: %q", c, plain)
			}
			if w := width(plain); w > c-1 {
				t.Errorf("%d: %d cells", c, w)
			}
		}
	}
}

func TestEngineMarks(t *testing.T) {
	tasks := []Task{{ID: 1, Mark: "✻", Title: "a", State: Working, Since: now}, {ID: 2, Mark: "⬡", Title: "b", State: Idle, Since: now}}
	rows := Layout(tasks, 80, now, true)
	if !strings.Contains(strip(rows[0]), "T1 ✻ a") || !strings.Contains(strip(rows[1]), "T2 ⬡ b") {
		t.Errorf("marks: %q", rows)
	}
	if strings.Contains(strip(Layout(twelve()[:3], 80, now, true)[0]), "✻") {
		t.Error("no marks when the tasks carry none")
	}
}
