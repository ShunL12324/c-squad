package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/flow"
	"github.com/ShunL12324/c-squad/internal/store"
)

// Review is the view key of a held task whose work is complete while tasks
// wait for it: "done · finish to start T18".
const Review = "review"

// queue is what the tasks queued after others need to show: the waiting
// tasks by the task they wait for, and the held tasks.
type queue struct {
	dependents map[int][]int
	held       map[int]bool
}

// queued reads the queue from every unfinished task.
func (a *App) queued(ctx context.Context) queue {
	q := queue{held: map[int]bool{}}
	db, err := a.Store()
	if err != nil {
		return q
	}
	tasks, _ := db.List(ctx, store.Filter{})
	q.dependents = waitingOn(tasks)
	for _, t := range tasks {
		if t.Hold {
			q.held[t.ID] = true
		}
	}
	return q
}

// reviewView shows what a hold blocks: a held task whose work is complete
// while tasks wait for it reads "done · finish to start T18"; a task waiting
// for a held one reads "waiting T17 · review".
func (q queue) reviewView(v View, t store.Task, l engine.Live, observed bool) View {
	switch t.Phase {
	case store.Waiting:
		for _, id := range t.After {
			if q.held[id] {
				return View{v.Key, v.Text + " · review"}
			}
		}
	case store.Running, store.Ready:
		deps := q.dependents[t.ID]
		if !t.Hold || len(deps) == 0 || t.SessionID == "" {
			return v
		}
		// The work is complete: the check passed, or, without one, the
		// worker stopped.
		complete := v.Key == store.Ready
		if t.Phase == store.Running && t.CheckSpec == "" {
			complete = observed && (l.State == engine.Idle || l.State == engine.Done)
		}
		if complete {
			return View{Review, "done · finish to start " + flow.Labels(deps)}
		}
	}
	return v
}

// nextStep is status's "next:" line for a task a hold blocks.
func (q queue) nextStep(t store.Task, v View) string {
	switch {
	case v.Key == Review:
		return fmt.Sprintf("review its work; csquad release %s merges it and starts %s (or merge it yourself, then csquad finish %s)",
			t.Label(), flow.Labels(q.dependents[t.ID]), t.Label())
	case t.Phase == store.Waiting:
		var held []int
		for _, id := range t.After {
			if q.held[id] {
				held = append(held, id)
			}
		}
		if len(held) > 0 {
			return fmt.Sprintf("%s waits for your review: csquad release %s (or finish it) starts this task", flow.Labels(held), flow.Labels(held))
		}
	}
	return ""
}

// implicitPlan adds to flowPlan the merge csquad makes for the tasks queued
// after t.
func (a *App) implicitPlan(q queue, t store.Task) string {
	deps := q.dependents[t.ID]
	if !flow.Implicit(t, deps) {
		return ""
	}
	into := "the main checkout's branch"
	if b, err := a.mergeTarget(t.Cwd, ""); err == nil {
		into = b
	}
	s := "merge into " + into + ", then " + flow.Labels(deps) + " starts"
	if t.InPlace {
		s = "done when the worker stops (in place), then " + flow.Labels(deps) + " starts"
	}
	if t.CheckSpec != "" {
		return " · " + s
	}
	return "\n  then: " + s
}

// Hold sets (or with off clears) a task's hold: a held task is never merged
// for the tasks queued after it; the user reviews it first.
func (a *App) Hold(ctx context.Context, label string, hold, all bool) error {
	verb := "hold"
	if !hold {
		verb = "release"
	}
	t, err := a.task(ctx, label, all, verb)
	if err != nil {
		return err
	}
	switch {
	case store.Finished(t.Phase):
		return fmt.Errorf("%s is already %s", t.Label(), t.Phase)
	case t.MergeInto != "":
		return fmt.Errorf("%s merges into %s by itself (launched with --merge); hold and release change the merge csquad makes for the tasks queued after a task", t.Label(), t.MergeInto)
	case hold && t.Phase == store.Merging:
		return fmt.Errorf("%s is already merging", t.Label())
	}
	db, err := a.Store()
	if err != nil {
		return err
	}
	if err := db.SetHold(ctx, t.ID, hold); err != nil {
		return err
	}
	deps := a.dependents(ctx, t.ID)
	if !hold {
		a.printf("released %s: csquad merges it when its work is done", t.Name())
		if len(deps) > 0 {
			a.printf(", then %s starts", flow.Labels(deps))
		}
		a.printf("\n")
		// It may merge now.
		a.AdvanceAll(ctx, nil)
		if cur, err := db.Get(ctx, t.ID); err == nil && cur.Phase != t.Phase {
			a.printf("%s is %s\n", cur.Label(), strings.ReplaceAll(cur.Phase, "_", " "))
		}
		return nil
	}
	a.printf("held %s for your review: csquad does not merge it", t.Name())
	if len(deps) > 0 {
		a.printf("; %s waits until you finish or release it", flow.Labels(deps))
	}
	a.printf("\n")
	return nil
}

// dependencyPlan is what launch says about each dependency of a queued
// task: how it gets done.
func (a *App) dependencyPlan(ctx context.Context, t store.Task) string {
	db, err := a.Store()
	if err != nil {
		return ""
	}
	var lines []string
	for _, id := range t.After {
		d, err := db.Get(ctx, id)
		if err != nil || d.Phase == store.Done {
			continue
		}
		switch {
		case d.MergeInto != "":
			lines = append(lines, fmt.Sprintf("%s merges into %s when it is done (--merge)", d.Label(), d.MergeInto))
		case d.Hold:
			lines = append(lines, fmt.Sprintf("%s is held for your review: csquad release %s merges it, or merge it and csquad finish %s", d.Label(), d.Label(), d.Label()))
		case d.InPlace:
			lines = append(lines, fmt.Sprintf("%s works in place: it is done when its worker stops", d.Label()))
		default:
			into := "the main checkout's branch"
			if b, err := a.mergeTarget(d.Cwd, ""); err == nil {
				into = b
			}
			lines = append(lines, fmt.Sprintf("%s merges into %s when it is done", d.Label(), into))
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return "\n  " + lines[0] + ", then " + t.Label() + " starts"
	}
	return "\n  " + strings.Join(lines, "\n  ") + "\n  then " + t.Label() + " starts"
}
