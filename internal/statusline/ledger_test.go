package statusline

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func strip(s string) string { return sgr.ReplaceAllString(s, "") }

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
				fmt.Fprintf(&b, "=== %d tasks, %d columns\n", n, c)
				for _, l := range Layout(twelve()[:n], c, now, noColor) {
					b.WriteString(strings.ReplaceAll(l, "\x1b", `\e`) + "\n")
				}
			}
		}
		name := "ledger-color.golden"
		if noColor {
			name = "ledger-nocolor.golden"
		}
		file := filepath.Join("testdata", name)
		if *update {
			os.MkdirAll("testdata", 0o755)
			if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != b.String() {
			t.Errorf("%s differs (go test -run TestGolden -update rewrites it):\n%s", file, b.String())
		}
	}
}

func TestColumnsFromWidth(t *testing.T) {
	cols := func(c int) int {
		return len(regexp.MustCompile(`   [▎▌] `).FindAllString(strip(Layout(twelve(), c, now, true)[0]), -1)) + 1
	}
	for c, want := range map[int]int{80: 1, 123: 1, 124: 2, 186: 2, 187: 3, 249: 3, 250: 4, 255: 4, 300: 4} {
		if got := cols(c); got != want {
			t.Errorf("%d columns: %d ledger columns, want %d", c, got, want)
		}
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

func TestNeverReachesTheLastColumn(t *testing.T) {
	long := append(twelve(), T(13, strings.Repeat("很长的标题", 30), Working, 1))
	for c := 1; c <= 300; c++ {
		for _, tasks := range [][]Task{twelve(), twelve()[:1], twelve()[:3], long} {
			for _, noColor := range []bool{true, false} {
				for _, r := range Layout(tasks, c, now, noColor) {
					if w := width(strip(r)); w > c-1 {
						t.Fatalf("width %d: row of %d cells: %q", c, w, strip(r))
					}
				}
			}
		}
	}
}

// The 1.x port filled a single column's whole width with the leader: dozens
// of dots after a short title. The title area follows the column's longest
// title instead, while states and ages stay aligned.
func TestShortLeaderInAWideColumn(t *testing.T) {
	tasks := []Task{T(1, "fix login", Working, 3), T(2, "add parser tests for empty input", NeedsInput, 18), T(10, "docs", Done, 65)}
	for _, c := range []int{80, 120, 123} {
		rows := Layout(tasks, c, now, true)
		var stateAt, ageEnd []int
		for _, r := range rows {
			plain := strip(r)
			if dots := strings.Count(plain, "·"); dots > len("add parser tests for empty input")-len("docs")+minLeader {
				t.Errorf("%d columns: %d dots in %q", c, dots, plain)
			}
			if !strings.Contains(plain, "··· ") && !strings.Contains(plain, "···  ") {
				t.Errorf("%d columns: no leader in %q", c, plain)
			}
			for _, s := range []string{"needs input", "working", "done"} {
				if i := strings.Index(plain, s); i >= 0 {
					stateAt = append(stateAt, width(plain[:i]))
				}
			}
			ageEnd = append(ageEnd, width(plain))
		}
		for i := range stateAt {
			if stateAt[i] != stateAt[0] || ageEnd[i] != ageEnd[0] {
				t.Errorf("%d columns: states or ages not aligned: %q", c, rows)
			}
		}
		if longest := strip(rows[0]); !strings.Contains(longest, "empty input ···  needs input") {
			t.Errorf("%d columns: the longest title should get a three-dot leader: %q", c, longest)
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
	if !regexp.MustCompile(`^▌ ◆ T1 tab here \[31m red ·+  needs input`).MatchString(strip(plain[0])) {
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
