package statusline

import (
	"fmt"
	"os"
	"path/filepath"
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
				fmt.Fprintf(&b, "=== %d tasks, %d columns\n", n, c)
				for _, l := range Layout(usageTasks()[:n], c, now, noColor) {
					b.WriteString(strings.ReplaceAll(l, "\x1b", `\e`) + "\n")
				}
			}
		}
		name := "ledger-usage-color.golden"
		if noColor {
			name = "ledger-usage-nocolor.golden"
		}
		file := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != b.String() {
			t.Errorf("%s differs (go test -run TestGoldenUsage -update rewrites it):\n%s", file, b.String())
		}
	}
}

func TestUsageCells(t *testing.T) {
	rows := Layout(usageTasks(), 255, now, true)
	plain := strip(strings.Join(rows, "\n"))
	for _, want := range []string{
		`working ▅▃▆▅▅▆▅▅ +42m`,                 // a full trail, newest on the right
		`working +▇▆·▇▆ +7m`,                    // a short history is right-aligned
		`fixing 2/3 +····· 3m +31m`,             // a stall: dots and the idle time
		`merging +▪▪·▪▪▪·▪ +3m`,                 // changes of unknown size
		`checking 1/3 +326k +12m`,               // not working: the total
		`done +1.2M 1h05m`, `failed +61k 2h10m`, // totals
		`idle +\? +5m`, `stopped +\? 4h00m`, // unknown totals
		`waiting T4 +7m`,                 // no session, no cell
		`needs you · merge confl… 1h35m`, // the badge runs across the cell
	} {
		if !regexp.MustCompile(want).MatchString(plain) {
			t.Errorf("no %q in\n%s", want, plain)
		}
	}
	// The stall's idle time is amber; changes are in the state's colour.
	colored := strings.Join(Layout(usageTasks(), 255, now, false), "\n")
	if !strings.Contains(colored, "\x1b[38;2;"+amber+"m3m\x1b[0m") || !strings.Contains(colored, "\x1b[38;2;"+blue+"m▅\x1b[0m") {
		t.Errorf("colours:\n%q", colored)
	}
}

// Below about 52 columns a cell steps down: the title shrinks first, then
// the usage cell narrows (short trail, newest change), then goes.
func TestUsageCellStepsDown(t *testing.T) {
	tasks := []Task{{ID: 1, Title: "Port the store to modernc sqlite", State: Working, Since: now.Add(-42 * time.Minute),
		Usage: usage(800_000, 0, 5000, 1500, 10000, 5000, 5000, 10000, 5000, 5000)}}
	cell := func(c int) string {
		row := strip(Layout(tasks, c, now, true)[0])
		m := regexp.MustCompile(`working( +[▁-█·▪]+)? +42m$`).FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("%d columns: %q", c, row)
		}
		return strings.TrimSpace(m[1])
	}
	if got := cell(60); got != "▅▃▆▅▅▆▅▅" {
		t.Errorf("60: %q", got)
	}
	last := 8
	for c := 60; c >= 30; c-- {
		n := len([]rune(cell(c)))
		if n > last {
			t.Errorf("%d columns: the cell widened to %d", c, n)
		}
		last = n
	}
	if got := cell(40); got != "▅▆▅▅" {
		t.Errorf("40: %q", got)
	}
	if got := cell(37); got != "▅" {
		t.Errorf("37: %q", got)
	}
	if got := cell(30); got != "" {
		t.Errorf("30: %q", got)
	}
	// A short title needs no more room: the whole trail stays.
	tasks[0].Title = "fix"
	if got := cell(40); got != "▅▃▆▅▅▆▅▅" {
		t.Errorf("40, short title: %q", got)
	}
}

func TestSamples(t *testing.T) {
	var list []Sample
	for i := 0; i < 12; i++ {
		list = AddSample(list, Sample{At: int64(i) * 10_000, Fresh: int64(i)})
	}
	if len(list) != MaxSamples || list[0].Fresh != 3 {
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

func TestStepsAndNumbers(t *testing.T) {
	for n, want := range map[int64]string{0: "·", unsized: "▪", 1: "▁", 499: "▁", 500: "▂", 999: "▂", 1000: "▃", 4000: "▅", 16000: "▇", 31999: "▇", 32000: "█", 1 << 30: "█"} {
		if got := step(n); got != want {
			t.Errorf("step(%d) = %s, want %s", n, got, want)
		}
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
	samples := []Sample{{Fresh: 10, Known: true}, {Fresh: 40_000, Known: true}, {Fresh: 40_000, Known: true, Activity: 5}, {Activity: 9}}
	if got := Trail(samples); got != "█·▪" {
		t.Errorf("trail %q", got)
	}
}
