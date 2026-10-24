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
	byPatch   map[string]string // branch → its head, verified in the target by patch only
	problems  []problem         // unmerged or uncommitted work: nothing may be removed
	ignored   []string          // refs named for the task but older than it: never touched
}

// problem is one reason a task's work is not in its target; detail names
// the commits.
type problem struct {
	text   string
	detail []string
}

func (p *plan) problem(detail []string, format string, args ...any) {
	p.problems = append(p.problems, problem{fmt.Sprintf(format, args...), detail})
}

// refs names the task's worktrees and branches.
func (p plan) refs() []string {
	var list []string
	for _, w := range p.worktrees {
		list = append(list, "worktree "+w.Path)
	}
	for _, b := range p.branches {
		list = append(list, "branch "+b)
	}
	return list
}

// summary is the problems on one line.
func (p plan) summary() string {
	var list []string
	for _, x := range p.problems {
		list = append(list, x.text)
	}
	return strings.Join(list, "; ")
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
			p.problem(nil, "branch %s is the target itself", b)
			continue
		}
		merged, byPatch, commits, err := g.Merged(p.repo, "refs/heads/"+b, target)
		if err != nil {
			return p, err
		}
		switch {
		case byPatch:
			// git branch -d refuses it: clean deletes it while it is at
			// the commit verified here.
			if p.byPatch == nil {
				p.byPatch = map[string]string{}
			}
			if p.byPatch[b], err = g.Run(p.repo, "rev-parse", "--verify", "-q", "refs/heads/"+b); err != nil {
				return p, err
			}
		case !merged:
			p.problem(commitLines(commits, target), "branch %s is not merged into %s", b, target)
		}
	}
	for _, w := range p.worktrees {
		if w.Detached && w.Head != "" {
			merged, _, commits, err := g.Merged(p.repo, w.Head, target)
			if err != nil {
				return p, err
			}
			if !merged {
				p.problem(commitLines(commits, target), "worktree %s has a detached HEAD %s not merged into %s", w.Path, short(w.Head), target)
			}
		}
		tracked, untracked, err := g.Changes(w.Path)
		if err != nil {
			return p, err
		}
		if len(tracked)+len(untracked) > 0 {
			p.problem(nil, "worktree %s has uncommitted changes: %s", w.Path, changeText(tracked, untracked))
		}
	}
	return p, nil
}

// listCap is about how many commits, files or directories a refusal names.
const listCap = 5

// commitLines names the commits not in target, oldest first, and counts
// those in it by patch.
func commitLines(commits []gitx.Commit, target string) []string {
	var out, rest []string
	inPatch := 0
	for _, c := range commits {
		if c.InPatch {
			inPatch++
		} else {
			rest = append(rest, c.Hash+" "+c.Subject)
		}
	}
	if len(rest) > 2*listCap {
		out = append(rest[:2*listCap], fmt.Sprintf("and %d more", len(rest)-2*listCap))
	} else {
		out = rest
	}
	if inPatch > 0 {
		out = append(out, fmt.Sprintf("%s already in %s by patch (cherry-picked)", plural(inPatch, "commit"), target))
	}
	return out
}

// changeText tells modified tracked files from untracked ones; untracked
// files are grouped by their top-level directory.
func changeText(tracked, untracked []string) string {
	var out []string
	if len(tracked) > 0 {
		out = append(out, "modified tracked files: "+capped(tracked))
	}
	if len(untracked) > 0 {
		var groups []string
		count := map[string]int{}
		for _, f := range untracked {
			g := f
			if dir, _, ok := strings.Cut(f, "/"); ok {
				g = dir + "/"
			}
			if count[g] == 0 {
				groups = append(groups, g)
			}
			count[g]++
		}
		for i, g := range groups {
			if strings.HasSuffix(g, "/") {
				groups[i] = fmt.Sprintf("%s (%s)", g, plural(count[g], "file"))
			}
		}
		if len(tracked) == 0 {
			out = append(out, "only untracked files: "+capped(groups))
		} else {
			out = append(out, "untracked files: "+capped(groups))
		}
	}
	return strings.Join(out, "; ")
}

func capped(list []string) string {
	if len(list) > listCap {
		return strings.Join(list[:listCap], ", ") + fmt.Sprintf(", and %d more", len(list)-listCap)
	}
	return strings.Join(list, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// refusal is finish's error for work not in the target: what blocks it,
// then both ways out.
func refusal(t store.Task, p plan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s is not finished, nothing was removed:", t.Label())
	for _, x := range p.problems {
		fmt.Fprintf(&b, "\n  - %s", x.text)
		if len(x.detail) > 0 {
			fmt.Fprintf(&b, ":\n      %s", strings.Join(x.detail, "\n      "))
		}
	}
	fmt.Fprintf(&b, "\n  keep the work and stop tracking it: csquad cancel %s", t.Label())
	fmt.Fprintf(&b, "\n  delete it:                          csquad cancel %s --discard", t.Label())
	return errors.New(b.String())
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
		if sha, ok := p.byPatch[b]; ok && !force {
			err = g.DeleteByPatch(p.repo, b, sha)
		} else if force {
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
		return refusal(t, p)
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
	a.removeFiles(t.ID, lock)
	a.printf("finished %s%s · removed %s\n", t.Name(), mergedText(p), orNone(done))
	if left != "" {
		a.printf("  the session is left: %s\n", left)
	}
	a.printIgnored(p.ignored)
	return nil
}

// mergedText says which branches are in the target, and which only by
// patch.
func mergedText(p plan) string {
	var plain, patch []string
	for _, b := range p.branches {
		if _, ok := p.byPatch[b]; ok {
			patch = append(patch, b)
		} else {
			plain = append(plain, b)
		}
	}
	s := ""
	if len(plain) > 0 {
		s += fmt.Sprintf(" · merged into %s: %s", p.target, strings.Join(plain, ", "))
	}
	if len(patch) > 0 {
		s += fmt.Sprintf(" · merged into %s by patch (cherry-picked/rebased): %s", p.target, strings.Join(patch, ", "))
	}
	return s
}

// Cancel takes a task out of the list (phase cancelled; the row keeps its
// number). Its session is stopped. A task with nothing to lose has its
// worktrees, branches and session removed as finish does; otherwise they are
// kept and cancel says where. With discard, the task's own refs go even when
// unmerged; an in-place task's checkout is never touched. Discard also
// deletes what an earlier cancel of the task kept.
func (a *App) Cancel(ctx context.Context, label string, discard, all bool) error {
	t, err := a.task(ctx, label, all, "cancel")
	if err != nil {
		return err
	}
	if t.Phase == store.Cancelled {
		return a.cancelAgain(ctx, t, discard)
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
			keep = fmt.Sprintf("kept its work (%s): %s\n  csquad cancel %s --discard deletes it, unmerged work included", strings.Join(p.refs(), ", "), p.summary(), t.Label())
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
	a.removeFiles(t.ID, lock)
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

// cancelAgain handles cancel on a cancelled task. With discard it deletes
// what the first cancel kept: the task's own worktrees and branches,
// unmerged ones included, then its session. Without discard it is an error
// that says whether work is left.
func (a *App) cancelAgain(ctx context.Context, t store.Task, discard bool) error {
	lock, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return fmt.Errorf("%s is busy; try again in a moment", t.Label())
	}
	defer func() { a.removeFiles(t.ID, lock) }()
	var l engine.Live
	var eng engine.Engine
	if t.SessionID != "" {
		if eng, err = a.Engine(t.Engine); err != nil {
			return err
		}
		live, err := a.observe(ctx, []store.Task{t})
		if err != nil {
			return err
		}
		l = live[t.ID]
	}
	p, err := a.inspect(t, "", l)
	if err != nil {
		return err
	}
	session := t.SessionID != "" && l.State != engine.Gone
	if !discard {
		left := p.refs()
		if session {
			left = append(left, "session "+t.SessionID)
		}
		if len(left) == 0 {
			return fmt.Errorf("%s is already cancelled", t.Label())
		}
		return fmt.Errorf("%s is already cancelled; its work is kept (%s)\n  csquad cancel %s --discard deletes it, unmerged work included", t.Label(), strings.Join(left, ", "), t.Label())
	}
	var done []string
	if session && (l.State == engine.Working || l.State == engine.NeedsInput || l.State == engine.Idle) {
		if err := eng.Stop(ctx, t.SessionID); err != nil {
			a.warnf("csquad: stopping %s: %v\n", t.Label(), err)
		} else {
			done = append(done, "stopped session "+t.SessionID)
		}
	}
	removed, gitDone, err := a.clean(p, true)
	done = append(done, gitDone...)
	if err != nil {
		return fmt.Errorf("%s: %v (removed so far: %s)", t.Label(), err, orNone(done))
	}
	left := ""
	if session {
		how, err := eng.Remove(ctx, t.SessionID, removed)
		if err != nil {
			left = err.Error()
		} else {
			done = append(done, how)
		}
	}
	a.printf("discarded %s · %s\n", t.Name(), orNone(done))
	if left != "" {
		a.printf("  the session is left: %s\n", left)
	}
	a.printIgnored(p.ignored)
	return nil
}

// clearGone takes out of the list the running tasks whose session was
// removed outside csquad and of which nothing is left: no worktree and no
// branch named for them. It returns the tasks still listed. A task with
// anything left stays, gone, until csquad cancel.
func (a *App) clearGone(ctx context.Context, tasks []store.Task, live map[int]engine.Live) []store.Task {
	var out []store.Task
	for _, t := range tasks {
		if l, ok := live[t.ID]; ok && l.State == engine.Gone && t.Phase == store.Running && a.clearIfEmpty(ctx, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (a *App) clearIfEmpty(ctx context.Context, t store.Task) bool {
	lock, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return false // csquad is starting, merging or cancelling it
	}
	defer lock.Release()
	if left, err := a.leftovers(t); err != nil || len(left) > 0 {
		return false
	}
	db, err := a.Store()
	if err != nil {
		return false
	}
	next := t
	next.Phase, next.Step, next.Note = store.Cancelled, "", "removed outside csquad"
	stored, err := db.Transition(ctx, t, next)
	if err != nil {
		return false
	}
	flow.Event(a.Home, t, stored, "removed outside csquad: no session, worktree or branch is left")
	a.removeFiles(t.ID, lock)
	return true
}

// leftovers names the worktrees and branches named for a task. A task in
// place has none of its own; neither has one whose directory is gone or
// outside git.
func (a *App) leftovers(t store.Task) ([]string, error) {
	if t.InPlace {
		return nil, nil
	}
	if _, err := os.Stat(t.Cwd); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	g := gitx.Git{Env: a.Env}
	top := g.TopLevel(t.Cwd)
	if top == "" {
		if _, err := g.Run(t.Cwd, "rev-parse", "--git-dir"); err != nil && !strings.Contains(err.Error(), "not a git repository") {
			return nil, err
		}
		return nil, nil
	}
	wts, err := g.Worktrees(top)
	if err != nil || len(wts) == 0 {
		return nil, err
	}
	refs, err := g.TaskRefs(wts[0].Path, t.ID, time.UnixMilli(t.CreatedAt))
	if err != nil {
		return nil, err
	}
	return plan{worktrees: refs.Worktrees, branches: refs.Branches}.refs(), nil
}

// printIgnored names the refs named for a task but older than it.
func (a *App) printIgnored(refs []string) {
	if len(refs) > 0 {
		a.printf("  ignored, older than the task (another task with its number made them): %s\n", strings.Join(refs, ", "))
	}
}

// removeFiles deletes a finished task's settings, prompt, lock and usage
// files. It releases the task lock first: Windows cannot delete an open
// file.
func (a *App) removeFiles(id int, lock *flock.Lock) {
	os.Remove(claude.SettingsFile(a.Home, id))
	os.Remove(claude.PromptFile(a.Home, id))
	lock.Release()
	os.Remove(a.TaskLock(id))
	os.Remove(a.usageState(id))
	os.Remove(a.usageBase(id))
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
