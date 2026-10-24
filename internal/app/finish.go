package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/engine/claude"
	"github.com/ShunL12324/c-squad/internal/flock"
	"github.com/ShunL12324/c-squad/internal/flow"
	"github.com/ShunL12324/c-squad/internal/gitx"
	"github.com/ShunL12324/c-squad/internal/store"
)

// plan is what finishing or cancelling a task would remove, after checking.
type plan struct {
	repo      string // "" outside git or for an in-place task
	target    string
	worktrees []gitx.Worktree
	branches  []string
	problems  []string // unmerged or uncommitted work: nothing may be removed
	ignored   []string // refs named for the task but older than it: never touched
}

// inspect finds what a task left in git and checks it against target ("" =
// the branch checked out in the main checkout).
func (a *App) inspect(t store.Task, target string, l engine.Live) (plan, error) {
	var p plan
	if t.InPlace {
		return p, nil // its changes are in the user's checkout, never csquad's to touch
	}
	g := gitx.Git{Env: a.Env}
	top := g.TopLevel(t.Cwd)
	if top == "" {
		return p, nil
	}
	wts, err := g.Worktrees(top)
	if err != nil {
		return p, err
	}
	if len(wts) == 0 {
		return p, nil
	}
	p.repo = wts[0].Path
	if target == "" {
		target = wts[0].Branch
		if target == "" {
			return p, fmt.Errorf("the main checkout %s is on a detached HEAD; pass --into BRANCH", p.repo)
		}
	}
	p.target = target
	refs, err := g.TaskRefs(p.repo, t.ID, time.UnixMilli(t.CreatedAt))
	if err != nil {
		return p, err
	}
	p.worktrees, p.branches, p.ignored = refs.Worktrees, refs.Branches, refs.Ignored
	// The session's own worktree belongs to the task whatever its name.
	if l.Cwd != "" {
		for _, w := range wts[1:] {
			if sameDir(w.Path, l.Cwd) && !hasWorktree(p.worktrees, w.Path) {
				p.worktrees = append(p.worktrees, w)
				if w.Branch != "" && !contains(p.branches, w.Branch) {
					p.branches = append(p.branches, w.Branch)
				}
			}
		}
	}
	for _, b := range p.branches {
		if b == target {
			p.problems = append(p.problems, fmt.Sprintf("branch %s is the target itself", b))
			continue
		}
		ok, err := g.IsAncestor(p.repo, "refs/heads/"+b, target)
		if err != nil {
			return p, err
		}
		if !ok {
			p.problems = append(p.problems, fmt.Sprintf("branch %s is not merged into %s", b, target))
		}
	}
	for _, w := range p.worktrees {
		if w.Detached && w.Head != "" {
			ok, err := g.IsAncestor(p.repo, w.Head, target)
			if err != nil {
				return p, err
			}
			if !ok {
				p.problems = append(p.problems, fmt.Sprintf("worktree %s has a detached HEAD %s not merged into %s", w.Path, short(w.Head), target))
			}
		}
		dirty, err := g.Dirty(w.Path)
		if err != nil {
			return p, err
		}
		if dirty != "" {
			p.problems = append(p.problems, fmt.Sprintf("worktree %s has uncommitted changes:\n%s", w.Path, indent(dirty)))
		}
	}
	return p, nil
}

// clean removes what inspect found, worktrees first, then branches; it
// returns the worktree paths removed.
func (a *App) clean(p plan, force bool) ([]string, []string, error) {
	g := gitx.Git{Env: a.Env}
	var removed, done []string
	for _, w := range p.worktrees {
		var err error
		if force {
			if w.Locked {
				_, _ = g.Run(p.repo, "worktree", "unlock", w.Path)
			}
			_, err = g.Run(p.repo, "worktree", "remove", "--force", w.Path)
		} else {
			err = g.RemoveWorktree(p.repo, w)
		}
		if err != nil {
			return removed, done, err
		}
		removed = append(removed, w.Path)
		done = append(done, "worktree "+w.Path)
	}
	if p.repo != "" {
		_ = g.Prune(p.repo)
	}
	for _, b := range p.branches {
		var err error
		if force {
			_, err = g.Run(p.repo, "branch", "-D", b)
		} else {
			err = g.DeleteMerged(p.repo, b, p.target)
		}
		if err != nil {
			return removed, done, err
		}
		done = append(done, "branch "+b)
	}
	return removed, done, nil
}

// Finish verifies that a task's work is merged and clean, then removes its
// worktrees and branches with git, then its session, and marks it done.
// Nothing is removed when something is unmerged or uncommitted.
func (a *App) Finish(ctx context.Context, labels []string, into string, all bool) error {
	failed := 0
	for _, label := range labels {
		if err := a.finishOne(ctx, label, into, all); err != nil {
			a.warnf("csquad: %v\n", err)
			failed++
		}
	}
	// Tasks queued after the finished ones may start now.
	a.AdvanceAll(ctx, nil)
	if failed > 0 {
		return errSilent
	}
	return nil
}

// errSilent: the error was already printed.
var errSilent = errors.New("")

// ErrSilent reports whether err was already printed.
func ErrSilent(err error) bool { return errors.Is(err, errSilent) }

func (a *App) finishOne(ctx context.Context, label, into string, all bool) error {
	t, err := a.task(ctx, label, all, "finish")
	if err != nil {
		return err
	}
	switch t.Phase {
	case store.Done:
		return fmt.Errorf("%s is already finished", t.Label())
	case store.Cancelled:
		return fmt.Errorf("%s was cancelled", t.Label())
	}
	if t.Phase == store.Waiting || t.SessionID == "" {
		return fmt.Errorf("%s has not started (finishing it would release the tasks queued after it); cancel it instead", t.Label())
	}
	// Claim it: the task lock, then the row as observed (a stale snapshot
	// or a cancel meanwhile fails the compare-and-set at the end).
	lock, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return fmt.Errorf("%s is busy (csquad is starting or merging it); try again in a moment", t.Label())
	}
	defer lock.Release()
	if t, err = a.task(ctx, label, true, "finish"); err != nil {
		return err
	}
	if store.Finished(t.Phase) {
		return fmt.Errorf("%s is already %s", t.Label(), t.Phase)
	}
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return err
	}
	live, err := a.observe(ctx, []store.Task{t})
	if err != nil {
		return err
	}
	l := live[t.ID]
	if l.State == engine.Working || l.State == engine.NeedsInput {
		return fmt.Errorf("%s is %s; finish it once it is done", t.Label(), stateText(l.State))
	}
	p, err := a.inspect(t, into, l)
	if err != nil {
		return err
	}
	if len(p.problems) > 0 {
		return fmt.Errorf("%s is not finished, nothing was removed:\n  - %s", t.Label(), strings.Join(p.problems, "\n  - "))
	}
	removed, done, err := a.clean(p, false)
	if err != nil {
		return fmt.Errorf("%s: %v (removed so far: %s)", t.Label(), err, orNone(done))
	}
	final := ""
	if msgs, err := eng.Last(ctx, t.SessionID, 0); err == nil {
		final = engine.LastAssistant(msgs)
	}
	left := ""
	if t.SessionID != "" && l.State != engine.Gone {
		how, err := eng.Remove(ctx, t.SessionID, removed)
		if err != nil {
			left = err.Error()
		} else {
			done = append(done, how)
		}
	}
	outcome, _ := json.Marshal(map[string]any{"target": p.target, "branches": p.branches, "final": final})
	next := t
	next.Phase, next.Outcome = store.Done, string(outcome)
	db, _ := a.Store()
	stored, err := db.Transition(ctx, t, next)
	if err != nil {
		return fmt.Errorf("%s: %v", t.Label(), err)
	}
	flow.Event(a.Home, t, stored, "finished")
	a.removeFiles(t.ID)
	merged := ""
	if len(p.branches) > 0 {
		merged = fmt.Sprintf(" · merged into %s: %s", p.target, strings.Join(p.branches, ", "))
	}
	a.printf("finished %s%s · removed %s\n", t.Name(), merged, orNone(done))
	if left != "" {
		a.printf("  the session is left: %s\n", left)
	}
	a.printIgnored(p.ignored)
	return nil
}

// Cancel takes a task out of the list (phase cancelled; the row keeps its
// number). Its session is stopped. A task with nothing to lose has its
// worktrees, branches and session removed as finish does; otherwise they are
// kept and cancel says where. With discard, the task's own refs go even when
// unmerged; an in-place task's checkout is never touched.
func (a *App) Cancel(ctx context.Context, label string, discard, all bool) error {
	t, err := a.task(ctx, label, all, "cancel")
	if err != nil {
		return err
	}
	if store.Finished(t.Phase) {
		return fmt.Errorf("%s is already %s", t.Label(), t.Phase)
	}
	lock, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return fmt.Errorf("%s is busy (csquad is starting or merging it); try again in a moment", t.Label())
	}
	defer lock.Release()
	db, err := a.Store()
	if err != nil {
		return err
	}
	var done, ignored []string
	keep := ""
	if t.SessionID != "" {
		eng, err := a.Engine(t.Engine)
		if err != nil {
			return err
		}
		live, err := a.observe(ctx, []store.Task{t})
		if err != nil {
			return err
		}
		l := live[t.ID]
		if l.State == engine.Working || l.State == engine.NeedsInput || l.State == engine.Idle {
			if err := eng.Stop(ctx, t.SessionID); err != nil {
				a.warnf("csquad: stopping %s: %v\n", t.Label(), err)
			} else {
				done = append(done, "stopped session "+t.SessionID)
			}
		}
		// Re-read after the interrupt: the task may have moved meanwhile.
		if t, err = db.Get(ctx, t.ID); err != nil {
			return err
		}
		if store.Finished(t.Phase) {
			return fmt.Errorf("%s is already %s", t.Label(), t.Phase)
		}
		p, err := a.inspect(t, "", l)
		if err != nil {
			return err
		}
		ignored = p.ignored
		if len(p.problems) > 0 && !discard {
			keep = fmt.Sprintf("kept its work: %s\n  csquad cancel %s --discard deletes it, unmerged work included", strings.Join(p.problems, "; "), t.Label())
		} else {
			removed, gitDone, err := a.clean(p, discard)
			done = append(done, gitDone...)
			if err != nil {
				return fmt.Errorf("%s: %v (removed so far: %s)", t.Label(), err, orNone(done))
			}
			if l.State != engine.Gone {
				how, err := eng.Remove(ctx, t.SessionID, removed)
				if err != nil {
					keep = "the session is left: " + err.Error()
				} else {
					done = append(done, how)
				}
			}
		}
	}
	next := t
	next.Phase, next.Step, next.Note = store.Cancelled, "", "cancelled"
	stored, err := db.Transition(ctx, t, next)
	if err != nil {
		return fmt.Errorf("%s: %v", t.Label(), err)
	}
	flow.Event(a.Home, t, stored, "cancelled")
	a.removeFiles(t.ID)
	a.printf("cancelled %s · %s\n", t.Name(), orNone(done))
	if keep != "" {
		a.printf("  %s\n", keep)
	}
	a.printIgnored(ignored)
	lock.Release()
	// Tasks queued after it stop for the user.
	a.AdvanceAll(ctx, nil)
	return nil
}

// printIgnored names the refs named for a task but older than it.
func (a *App) printIgnored(refs []string) {
	if len(refs) > 0 {
		a.printf("  ignored, older than the task (another task with its number made them): %s\n", strings.Join(refs, ", "))
	}
}

// removeFiles deletes a finished task's settings, prompt, lock and usage
// files.
func (a *App) removeFiles(id int) {
	os.Remove(claude.SettingsFile(a.Home, id))
	os.Remove(claude.PromptFile(a.Home, id))
	os.Remove(a.TaskLock(id))
	os.Remove(a.usageState(id))
	os.Remove(filepath.Join(a.cacheDir(), fmt.Sprintf("T%d.usage.json", id)))
	locks, _ := filepath.Glob(filepath.Join(a.logsDir(), fmt.Sprintf("T%d-check-*.lock", id)))
	for _, l := range locks {
		os.Remove(l)
	}
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "nothing"
	}
	return strings.Join(list, ", ")
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n      ")
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func sameDir(a, b string) bool {
	return filepath.Clean(canonical(a)) == filepath.Clean(canonical(b))
}

func hasWorktree(list []gitx.Worktree, path string) bool {
	for _, w := range list {
		if sameDir(w.Path, path) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
