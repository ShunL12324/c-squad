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
	"github.com/ShunL12324/c-squad/internal/flow"
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
	"ready":           "ready",
	"needs_you":       "needs you",
}

// flowState is what wait watches for a task csquad's flow drives: its
// phase, with the session's own state only where the user can act on it.
// An idle worker between the flow's steps is still working.
func flowState(t store.Task, live string) string {
	switch t.Phase {
	case store.Ready:
		return "ready"
	case store.NeedsYou:
		return "needs_you"
	case store.Failed:
		return engine.Failed
	case store.Waiting, store.Checking:
		return engine.Working
	}
	switch live {
	case engine.NeedsInput, engine.Failed, engine.Gone, engine.Stopped:
		return live
	}
	return engine.Working
}

func (a *App) envMs(name string, fallback time.Duration) time.Duration {
	if v, err := strconv.Atoi(a.Env.Get(name)); err == nil && v >= 0 {
		return time.Duration(v) * time.Millisecond
	}
	return fallback
}

func noteOf(t store.Task) string {
	if t.Phase == store.NeedsYou && t.Note != "" {
		return " · " + t.Note
	}
	return ""
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
	var stopAt time.Time
	firstFailure := map[string]time.Time{} // by engine
	db, err := a.Store()
	if err != nil {
		return err
	}
	for {
		a.AdvanceAll(ctx, nil)
		var current []store.Task
		for _, w := range ws {
			if t, err := db.Get(ctx, w.t.ID); err == nil {
				w.t = t
			}
			current = append(current, w.t)
		}
		live, oerr := a.observe(ctx, current)
		failing := engineErrors(oerr)
		// Only an engine a watched, unfinished task needs can fail wait; its
		// tasks sit this poll out, the other engines' tasks go on.
		for name := range firstFailure {
			if failing[name] == nil {
				delete(firstFailure, name)
			}
		}
		for _, w := range ws {
			err := failing[w.t.Engine]
			if w.done || err == nil || w.t.SessionID == "" || store.Finished(w.t.Phase) {
				continue
			}
			if _, ok := firstFailure[w.t.Engine]; !ok {
				firstFailure[w.t.Engine] = a.Now()
			}
			if a.Now().Sub(firstFailure[w.t.Engine]) >= failLimit {
				return fmt.Errorf("the %s engine keeps failing: %v", w.t.Engine, err)
			}
		}
		for _, w := range ws {
			if w.done || failing[w.t.Engine] != nil && w.t.SessionID != "" && !store.Finished(w.t.Phase) {
				continue
			}
			state := live[w.t.ID].State
			switch {
			case store.Finished(w.t.Phase):
				state = "finished"
			case flow.Orchestrated(w.t) || w.t.Phase == store.Waiting:
				state = flowState(w.t, state)
			case w.t.SessionID == "":
				state = "starting"
			}
			if !w.inited {
				w.inited, w.state, w.cand = true, state, state
				if text, ok := report[state]; ok {
					w.done = true
					events = append(events, struct {
						id   int
						text string
					}{w.t.ID, fmt.Sprintf("%s · already %s%s", w.t.Name(), text, noteOf(w.t))})
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
				}{w.t.ID, fmt.Sprintf("%s · %s%s", w.t.Name(), text, noteOf(w.t))})
			}
		}
		if len(events) > 0 && stopAt.IsZero() {
			stopAt = a.Now().Add(grace)
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
