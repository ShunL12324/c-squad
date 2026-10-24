package statusline

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// usage builds a usage cell ending at now from the changes of the last
// refreshes, oldest first: tokens, 0 for nothing, -1 for a change of unknown
// size (a gateway that reports no usage). idle > 0 dates the last activity.
func usage(total int64, idle time.Duration, changes ...int64) *Usage {
	known := true
	sum := int64(0)
	for _, c := range changes {
		if c < 0 {
			known = false
		} else {
			sum += c
		}
	}
	at := now.Add(-10 * time.Second * time.Duration(len(changes)))
	act := at.Add(-time.Hour)
	s := Sample{At: at.UnixMilli(), Fresh: total - sum, Known: known, Activity: act.UnixMilli()}
	samples := []Sample{s}
	for _, c := range changes {
		at = at.Add(10 * time.Second)
		s.At = at.UnixMilli()
		if c != 0 {
			act = at
			s.Activity = act.UnixMilli()
		}
		if c > 0 {
			s.Fresh += c
		}
		samples = append(samples, s)
	}
	if idle > 0 {
		act = now.Add(-idle)
	}
	return &Usage{Samples: samples, Known: known, Total: total, Activity: act}
}

// usageTasks are the tasks of the 2026-10-24 samples of #41: the usage cell,
// engine marks, a stall and a gateway that reports no usage.
func usageTasks() []Task {
	tasks := twelve()
	cells := map[int]*Usage{
		2:  usage(90_000, 0, 1000),
		9:  usage(400_000, 0),
		10: usage(61_000, 0),
		3:  usage(1_200_000, 0),
		1:  usage(800_000, 0, 5000, 1500, 10000, 5000, 5000, 10000, 5000, 5000),
		4:  usage(120_000, 0, 20000, 10000, 0, 20000, 10000),
		6:  usage(326_000, 0),
		7:  usage(210_000, 3*time.Minute, 0, 0, 0),
		8:  usage(0, 0, -1, -1, 0, -1, -1, -1, 0, -1),
		12: usage(0, 0, -1),
		11: usage(0, 0, -1),
	}
	for i := range tasks {
		tasks[i].Usage = cells[tasks[i].ID]
		tasks[i].Mark = "✻"
		if tasks[i].ID == 4 || tasks[i].ID == 6 || tasks[i].ID == 11 {
			tasks[i].Mark = "⬡"
		}
	}
	return tasks
}

// TestGoldenUsage pins the ledger with usage cells at 50/80/120/160/240/255
// columns x 0/1/3/6/12 tasks x colour/NO_COLOR (-update rewrites it).
func TestGoldenUsage(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		var b strings.Builder
		for _, n := range []int{0, 1, 3, 6, 12} {
			for _, c := range []int{50, 80, 120, 160, 240, 255} {
				goldenText(&b, fmt.Sprintf("%d tasks, %d columns", n, c), usageTasks()[:n], c, noColor)
			}
		}
		checkGolden(t, goldenName("ledger-usage", noColor), b.String())
	}
}

func TestUsageCells(t *testing.T) {
	rows := Layout(usageTasks(), 255, now, true)
	plain := strip(strings.Join(rows, "\n"))
	for _, want := range []string{
		`working +800k +\+5\.0k +42m`,              // the total, then the change since the last refresh
		`working +120k +\+10k +7m`,                 //
		`fixing 2/3 +210k +idle 3m +31m`,           // a stall: the idle time
		`merging +\? +\+\? +3m`,                    // a change of unknown size
		`checking 1/3 +326k +12m`,                  // one reading: no change yet
		`done +1\.2M +1h05m`, `failed +61k +2h10m`, // totals
		`idle +\? +5m`, `stopped +\? +4h00m`, // unknown totals
		`waiting T4 +7m`,                    // no session, no cell
		`needs you · merge conflict  1h35m`, // the badge runs across the cell
	} {
		if !regexp.MustCompile(want).MatchString(plain) {
			t.Errorf("no %q in\n%s", want, plain)
		}
	}
	// The idle time is amber; a change is in the state's colour; +0 is faint.
	colored := strings.Join(Layout(usageTasks(), 255, now, false), "\n")
	if !strings.Contains(colored, "\x1b[38;2;"+amber+"midle 3m\x1b[0m") || !strings.Contains(colored, "\x1b[38;2;"+blue+"m+5.0k\x1b[0m") {
		t.Errorf("colours:\n%q", colored)
	}
	still := []Task{{ID: 1, Title: "x", State: Working, Since: now, Usage: usage(5000, 0, 100, 0)}}
	if r := Layout(still, 80, now, false)[0]; !strings.Contains(r, "\x1b[2m+0\x1b[0m") || !regexp.MustCompile(`working +5\.0k +\+0 +now$`).MatchString(strip(r)) {
		t.Errorf("+0: %q", r)
	}
}

// Below 51 columns a cell steps down: the title shrinks first, then
// the change goes, then the total.
func TestUsageCellStepsDown(t *testing.T) {
	tasks := []Task{{ID: 1, Title: "Port the store to modernc sqlite", State: Working, Since: now.Add(-42 * time.Minute),
		Usage: usage(800_000, 0, 5000, 1500, 10000, 5000, 5000, 10000, 5000, 5000)}}
	cell := func(c int) string {
		row := strip(Layout(tasks, c, now, true)[0])
		m := regexp.MustCompile(`working( +[0-9.kM?]+)?( +\+[0-9.kM?]+)? +42m$`).FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("%d columns: %q", c, row)
		}
		return strings.TrimSpace(m[1] + m[2])
	}
	if got := cell(60); got != "800k +5.0k" {
		t.Errorf("60: %q", got)
	}
	last := len("800k +5.0k")
	for c := 60; c >= 30; c-- {
		n := len(cell(c))
		if n > last {
			t.Errorf("%d columns: the cell widened to %d", c, n)
		}
		last = n
	}
	if got := cell(51); got != "800k +5.0k" {
		t.Errorf("51: %q", got)
	}
	if got := cell(50); got != "800k" {
		t.Errorf("50: %q", got)
	}
	if got := cell(43); got != "800k" {
		t.Errorf("43: %q", got)
	}
	if got := cell(42); got != "" {
		t.Errorf("42: %q", got)
	}
	// A short title needs no more room: the whole cell stays.
	tasks[0].Title = "fix"
	if got := cell(42); got != "800k +5.0k" {
		t.Errorf("42, short title: %q", got)
	}
}

func TestSamples(t *testing.T) {
	var list []Sample
	for i := 0; i < 12; i++ {
		list = AddSample(list, Sample{At: int64(i) * 10_000, Fresh: int64(i)})
	}
	if len(list) != MaxSamples || list[0].Fresh != 10 {
		t.Errorf("%+v", list)
	}
	if got := AddSample(list, Sample{At: list[len(list)-1].At + 7_999}); len(got) != MaxSamples || got[len(got)-1] != list[len(list)-1] {
		t.Error("a reading less than 8 s after the newest is kept")
	}
	dir := t.TempDir()
	saveSamples(dir, 12, "sess", list)
	if got := LoadSamples(dir, 12, "sess"); len(got) != MaxSamples {
		t.Errorf("loaded %v", got)
	}
	if got := LoadSamples(dir, 12, "other"); got != nil {
		t.Error("samples of another session")
	}
}

func TestChangeAndNumbers(t *testing.T) {
	for _, c := range []struct {
		samples []Sample
		n       int64
		ok      bool
		text    string
	}{
		{nil, 0, false, ""},
		{[]Sample{{Fresh: 10, Known: true}}, 0, false, ""},
		{[]Sample{{At: 0, Fresh: 10, Known: true}, {At: 10_000, Fresh: 4110, Known: true}}, 4100, true, "+4.1k"},
		{[]Sample{{Fresh: 40_000, Known: true}, {Fresh: 40_000, Known: true, Activity: 5}}, 0, true, "+0"},
		{[]Sample{{Fresh: 40_000, Known: true}, {Activity: 9}}, Unsized, true, "+?"},
		{[]Sample{{Activity: 9}, {Activity: 9}}, 0, true, "+0"},
		// Samples kept by 3.0.0 (up to 9): the last two count.
		{[]Sample{{Fresh: 0, Known: true}, {Fresh: 5, Known: true}, {Fresh: 326_405, Known: true}}, 326_400, true, "+326k"},
	} {
		n, _, ok := Change(c.samples)
		if n != c.n || ok != c.ok || ok && ChangeText(n) != c.text {
			t.Errorf("%+v: %d %v %s", c.samples, n, ok, ChangeText(n))
		}
	}
	if _, span, _ := Change([]Sample{{At: 1000}, {At: 11_000}}); span != 10*time.Second {
		t.Errorf("span %v", span)
	}
	for n, want := range map[int64]string{622: "622", 4100: "4.1k", 9949: "9.9k", 61_000: "61k", 326_400: "326k", 999_499: "999k", 999_500: "1.0M", 1_200_000: "1.2M", 12_000_000: "12M"} {
		if got := Tokens(n); got != want {
			t.Errorf("Tokens(%d) = %s, want %s", n, got, want)
		}
	}
	for n, want := range map[int64]string{4: "4", 622: "622", 51_100: "51.1k", 51_800: "51.8k", 221_000: "221k", 3_050_000: "3.05M", 60_600_000: "60.6M", 9_996: "10.0k"} {
		if got := TokensExact(n); got != want {
			t.Errorf("TokensExact(%d) = %s, want %s", n, got, want)
		}
	}
}
