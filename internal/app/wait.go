package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/store"
)

// report: the states wait exits for.
var report = map[string]string{
	engine.NeedsInput: "needs input",
	engine.Idle:       "idle",
	engine.Done:       "done",
	engine.Failed:     "failed",
	engine.Stopped:    "stopped",
	engine.Gone:       "removed",
	"finished":        "finished",
}

func (a *App) envMs(name string, fallback time.Duration) time.Duration {
	if v, err := strconv.Atoi(a.Env.Get(name)); err == nil && v >= 0 {
		return time.Duration(v) * time.Millisecond
	}
	return fallback
}

func span(minutes float64) string {
	if minutes < 120 {
		return fmt.Sprintf("%.0fm", minutes)
	}
	return fmt.Sprintf("%.0fh", minutes/60)
}

// Wait is an opt-in monitor: it polls the engines without any model
// involvement and exits once a named task changes into a state the user must
// hear about. A new state counts only when seen on two consecutive polls;
// events within a short grace period share one exit.
func (a *App) Wait(ctx context.Context, labels []string, timeoutMin float64, all bool) error {
	if len(labels) == 0 {
		return fmt.Errorf("usage: csquad wait T12 [T13 ...] [--timeout MINUTES] [--all]")
	}
	if !(timeoutMin > 0) {
		return fmt.Errorf("--timeout expects a number of minutes")
	}
	interval := a.envMs("CSQUAD_WAIT_INTERVAL_MS", 10*time.Second)
	grace := a.envMs("CSQUAD_WAIT_GRACE_MS", 5*time.Second)
	failLimit := a.envMs("CSQUAD_WAIT_FAIL_MS", 2*time.Minute)

	var tasks []store.Task
	seen := map[int]bool{}
	for _, l := range labels {
		t, err := a.task(ctx, l, all, "wait for")
		if err != nil {
			return err
		}
		if !seen[t.ID] {
			seen[t.ID] = true
			tasks = append(tasks, t)
		}
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	type watch struct {
		t            store.Task
		state, cand  string
		count        int
		done, inited bool
	}
	ws := make([]*watch, len(tasks))
	for i, t := range tasks {
		ws[i] = &watch{t: t}
	}
	var events []struct {
		id   int
		text string
	}
	start := a.Now()
	deadline := start.Add(time.Duration(timeoutMin * float64(time.Minute)))
	var firstFailure, stopAt time.Time
	db, err := a.Store()
	if err != nil {
		return err
	}
	for {
		var current []store.Task
		for _, w := range ws {
			if t, err := db.Get(ctx, w.t.ID); err == nil {
				w.t = t
			}
			current = append(current, w.t)
		}
		live, oerr := a.observe(ctx, current)
		if oerr != nil {
			if firstFailure.IsZero() {
				firstFailure = a.Now()
			}
			if a.Now().Sub(firstFailure) >= failLimit {
				return fmt.Errorf("%s keeps failing: %v", "the engine", oerr)
			}
		} else {
			firstFailure = time.Time{}
			for _, w := range ws {
				if w.done {
					continue
				}
				state := live[w.t.ID].State
				if store.Finished(w.t.Phase) {
					state = "finished"
				} else if w.t.SessionID == "" {
					state = "starting"
				}
				if !w.inited {
					w.inited, w.state, w.cand = true, state, state
					if text, ok := report[state]; ok {
						w.done = true
						events = append(events, struct {
							id   int
							text string
						}{w.t.ID, fmt.Sprintf("%s · already %s", w.t.Name(), text)})
					}
					continue
				}
				if state == w.state {
					w.cand, w.count = state, 0
					continue
				}
				if state == w.cand {
					w.count++
				} else {
					w.cand, w.count = state, 1
				}
				if w.count < 2 {
					continue
				}
				w.state, w.count = state, 0
				if text, ok := report[state]; ok {
					w.done = true
					events = append(events, struct {
						id   int
						text string
					}{w.t.ID, fmt.Sprintf("%s · %s", w.t.Name(), text)})
				}
			}
			if len(events) > 0 && stopAt.IsZero() {
				stopAt = a.Now().Add(grace)
			}
		}
		now := a.Now()
		allDone := true
		for _, w := range ws {
			allDone = allDone && w.done
		}
		if len(events) > 0 && !now.Before(stopAt) || allDone {
			break
		}
		if !now.Before(deadline) {
			for _, w := range ws {
				if !w.done {
					events = append(events, struct {
						id   int
						text string
					}{w.t.ID, fmt.Sprintf("%s · still working after %s", w.t.Name(), span(timeoutMin))})
				}
			}
			break
		}
		next := interval
		if len(events) > 0 {
			next = min(next, stopAt.Sub(now))
		} else {
			next = min(next, deadline.Sub(now))
		}
		select {
		case <-ctx.Done():
			return nil // SIGTERM/SIGINT: exit quietly
		case <-time.After(max(next, time.Millisecond)):
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].id < events[j].id })
	var lines []string
	for _, e := range events {
		lines = append(lines, e.text)
	}
	a.printf("%s\n", strings.Join(lines, "\n"))
	return nil
}
