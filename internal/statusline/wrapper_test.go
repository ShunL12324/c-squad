package statusline

import (
	"context"
	"errors"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

func TestRunFallsBackToTheLastGoodOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	clock := now
	tasks := []Observed{{Task: Task{ID: 1, Title: "job", State: Working}, SessionID: "s", StartedAt: now.Add(-5 * time.Minute)}}
	fail := false
	o := Options{
		Stdin: []byte(`{"session_id":"abc"}`), Command: "echo mine", Env: os.Environ(), CacheDir: dir, Columns: 80, NoColor: true,
		Now: func() time.Time { return clock },
		Query: func(context.Context, Input) ([]Observed, error) {
			if fail {
				return nil, errors.New("down")
			}
			return tasks, nil
		},
	}
	out := strip(Run(o))
	if !strings.HasPrefix(out, "mine\n▎ ● T1 job ") || !regexp.MustCompile(`working +5m`).MatchString(out) {
		t.Fatalf("%q", out)
	}
	fail = true
	o.Command = "exit 1"
	clock = now.Add(time.Minute)
	if got := strip(Run(o)); !strings.HasPrefix(got, "mine\n▎ ● T1 job") {
		t.Errorf("within 2 minutes: %q", got)
	}
	clock = now.Add(3 * time.Minute)
	if got := Run(o); got != "" {
		t.Errorf("after 2 minutes the stale parts go: %q", got)
	}
	// A different session has its own cache.
	o.Stdin = []byte(`{"session_id":"other"}`)
	if got := Run(o); got != "" {
		t.Errorf("%q", got)
	}
}

func TestRunRemembersTheTimeInState(t *testing.T) {
	dir := t.TempDir()
	clock := now
	state := Working
	o := Options{
		Stdin: []byte(`{"session_id":"abc"}`), CacheDir: dir, Columns: 80, NoColor: true,
		Now: func() time.Time { return clock },
		Query: func(context.Context, Input) ([]Observed, error) {
			return []Observed{{Task: Task{ID: 1, Title: "job", State: state}, SessionID: "s", StartedAt: now.Add(-2 * time.Hour),
				Activity: func() (time.Time, bool) { return now.Add(-30 * time.Minute), true }}}, nil
		},
	}
	if got := strip(Run(o)); !regexp.MustCompile(`working +2h00m`).MatchString(got) {
		t.Errorf("first seen working: since its start: %q", got)
	}
	clock = now.Add(10 * time.Minute)
	state = Idle
	if got := strip(Run(o)); !regexp.MustCompile(`idle +now`).MatchString(got) {
		t.Errorf("a change dates from now: %q", got)
	}
	clock = now.Add(15 * time.Minute)
	if got := strip(Run(o)); !regexp.MustCompile(`idle +5m`).MatchString(got) {
		t.Errorf("unchanged keeps its time: %q", got)
	}
	// A state seen for the first time (a fresh cache) dates from the last activity.
	o.CacheDir = t.TempDir()
	if got := strip(Run(o)); !regexp.MustCompile(`idle +45m`).MatchString(got) {
		t.Errorf("first seen idle: since its last activity: %q", got)
	}
}

func TestRunWhenTheServiceDoesNotAnswer(t *testing.T) {
	dir := t.TempDir()
	clock := now
	var qerr error
	o := Options{
		Stdin: []byte(`{"session_id":"abc"}`), CacheDir: dir, Columns: 80, NoColor: true,
		Now: func() time.Time { return clock },
		Query: func(context.Context, Input) ([]Observed, error) {
			if qerr != nil {
				return nil, qerr
			}
			return []Observed{{Task: Task{ID: 1, Title: "job", State: Working}, SessionID: "s", StartedAt: now}}, nil
		},
	}
	if got := strip(Run(o)); !regexp.MustCompile(`^▎ ● T1 job +working +now\n$`).MatchString(got) {
		t.Fatalf("%q", got)
	}
	qerr = &engine.NotAnswering{Service: "Claude Code's background service", Short: "claude agents not answering"}
	clock = now.Add(time.Minute)
	if got := strip(Run(o)); !regexp.MustCompile(`^▎ ● T1 job +working\? +1m\n$`).MatchString(got) {
		t.Errorf("within 2 minutes the last rows show, marked: %q", got)
	}
	clock = now.Add(3 * time.Minute)
	if got := strip(Run(o)); got != "▎ ? claude agents not answering — csquad status for details\n" {
		t.Errorf("after 2 minutes one row says why: %q", got)
	}
}
