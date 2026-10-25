package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/check"
	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/flock"
	"github.com/ShunL12324/c-squad/internal/flow"
	"github.com/ShunL12324/c-squad/internal/gitx"
	"github.com/ShunL12324/c-squad/internal/proc"
	"github.com/ShunL12324/c-squad/internal/store"
)

func (a *App) logsDir() string { return filepath.Join(a.Home, "logs") }

// TaskLock is held while a task's session starts, its merge lands or it is
// finished.
func (a *App) TaskLock(id int) string { return filepath.Join(a.logsDir(), fmt.Sprintf("T%d.lock", id)) }

// CheckLock is held while the check of a round runs.
func (a *App) CheckLock(id, round int) string {
	return filepath.Join(a.logsDir(), fmt.Sprintf("T%d-check-%d.lock", id, round))
}

func (a *App) checkLog(id, round int) string {
	return filepath.Join(a.logsDir(), fmt.Sprintf("T%d-check-%d.log", id, round))
}

// VerdictFile is where a check leaves its verdict.
func (a *App) VerdictFile(id, round int) string {
	return filepath.Join(a.logsDir(), fmt.Sprintf("T%d-check-%d.json", id, round))
}

// AdvanceLock is held by `csquad advance`.
func (a *App) AdvanceLock() string { return filepath.Join(a.logsDir(), "advance.lock") }

func (a *App) settle() time.Duration { return a.envMs("CSQUAD_SETTLE_MS", 10*time.Second) }
func (a *App) grace() time.Duration  { return a.envMs("CSQUAD_CHECK_GRACE_MS", 30*time.Second) }

func (a *App) mover() *flow.Mover {
	db, _ := a.Store()
	return &flow.Mover{DB: db, Home: a.Home, Run: a.runEffect}
}

// needsObservation: the flow reads these tasks' live session state.
// dependents holds the waiting tasks by the task they wait for.
func needsObservation(t store.Task, dependents map[int][]int) bool {
	if t.SessionID == "" || !flow.Orchestrated(t) && !flow.Implicit(t, dependents[t.ID]) {
		return false
	}
	switch t.Phase {
	case store.Running, store.Fixing, store.Merging, store.NeedsYou, store.Failed, store.Ready:
		return true
	}
	return false
}

// waitingOn maps each task to the waiting tasks among tasks that list it in
// their After.
func waitingOn(tasks []store.Task) map[int][]int {
	out := map[int][]int{}
	for _, t := range tasks {
		if t.Phase != store.Waiting {
			continue
		}
		for _, id := range t.After {
			out[id] = append(out[id], t.ID)
		}
	}
	return out
}

// dependents are the waiting tasks queued after task id.
func (a *App) dependents(ctx context.Context, id int) []int {
	db, err := a.Store()
	if err != nil {
		return nil
	}
	tasks, err := db.List(ctx, store.Filter{})
	if err != nil {
		return nil
	}
	return waitingOn(tasks)[id]
}

// observation gathers what Next needs about t. live holds the engine states
// of the tasks that were observed.
func (a *App) observation(ctx context.Context, t store.Task, live map[int]engine.Live, stopped map[int]time.Time) flow.Obs {
	o := flow.Obs{Now: a.Now(), Settle: a.settle(), Grace: a.grace(), Stopped: stopped[t.ID]}
	o.Live, o.Observed = live[t.ID]
	db, _ := a.Store()
	if t.MergeInto == "" && !t.Hold && (t.Phase == store.Running || t.Phase == store.Checking || t.Phase == store.Ready) {
		// Whether the work merges for the tasks queued after it.
		o.Dependents = a.dependents(ctx, t.ID)
	}
	switch t.Phase {
	case store.Waiting:
		o.Deps = map[int]store.Task{}
		for _, id := range t.After {
			if d, err := db.Get(ctx, id); err == nil {
				o.Deps[id] = d
			}
		}
	case store.Running:
		if t.SessionID == "" {
			o.Busy = flock.Held(a.TaskLock(t.ID))
		}
	case store.Checking:
		o.Busy = flock.Held(a.CheckLock(t.ID, t.Round))
		if st, err := os.Stat(a.checkLog(t.ID, t.Round)); err == nil {
			o.Quiet = st.ModTime()
		}
		if v, ok := a.readVerdict(t); ok {
			o.Verdict = &v
		}
	case store.Merging:
		o.Busy = flock.Held(a.TaskLock(t.ID))
	}
	if (t.Phase == store.Fixing || t.Phase == store.Merging && t.Step == flow.Asked) && o.Observed &&
		(o.Live.State == engine.Idle || o.Live.State == engine.Done) && !o.Stopped.After(time.UnixMilli(t.UpdatedAt)) {
		o.Replied = a.replied(ctx, t)
	}
	return o
}

// readVerdict reads the verdict the check of t's round left for this
// version of the task.
func (a *App) readVerdict(t store.Task) (check.Verdict, bool) {
	var v check.Verdict
	b, err := os.ReadFile(a.VerdictFile(t.ID, t.Round))
	if err != nil || json.Unmarshal(b, &v) != nil || v.For != t.UpdatedAt || v.Round != t.Round {
		return v, false
	}
	return v, true
}

// replied reports whether the worker answered after the task entered its
// step: an assistant message dated after it, or, from an engine that dates
// nothing, an assistant message after csquad's last message.
func (a *App) replied(ctx context.Context, t store.Task) bool {
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return false
	}
	msgs, err := eng.Last(ctx, t.SessionID, 0)
	if err != nil {
		return false
	}
	since := time.UnixMilli(t.UpdatedAt)
	dated := false
	for _, m := range msgs {
		if !m.At.IsZero() {
			dated = true
			if m.Role == "assistant" && m.At.After(since) {
				return true
			}
		}
	}
	if dated {
		return false
	}
	prefix := "csquad: the work is accepted"
	if t.Phase == store.Fixing {
		prefix = fmt.Sprintf("csquad: check %d of ", t.Round-1)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && strings.HasPrefix(msgs[i].Text, prefix) {
			for _, m := range msgs[i+1:] {
				if m.Role == "assistant" {
					return true
				}
			}
			return false
		}
	}
	return false
}

// AdvanceAll moves every unfinished task as far as what is observed allows,
// running the effects of the moves this process won. stopped holds Stop hook
// events by task. Effect errors land in the tasks' notes; nothing fails.
func (a *App) AdvanceAll(ctx context.Context, stopped map[int]time.Time) {
	if _, err := os.Stat(a.DBFile()); err != nil {
		return
	}
	db, err := a.Store()
	if err != nil {
		return
	}
	// A task that lands releases its dependents: look again, a few times.
	for pass := 0; pass < 4; pass++ {
		tasks, err := db.List(ctx, store.Filter{})
		if err != nil {
			return
		}
		var watched []store.Task
		dependents := waitingOn(tasks)
		for _, t := range tasks {
			if needsObservation(t, dependents) {
				watched = append(watched, t)
			}
		}
		live := map[int]engine.Live{}
		if len(watched) > 0 {
			live, _ = a.observe(ctx, watched)
		}
		moved := false
		for _, t := range tasks {
			o := a.observation(ctx, t, live, stopped)
			m := flow.Next(t, o)
			if !m.Acts(t) {
				continue
			}
			_, won, err := a.mover().Apply(ctx, t, m)
			if err != nil && !errors.Is(err, store.ErrConflict) {
				a.warnf("csquad: %s: %v\n", t.Label(), err)
			}
			moved = moved || won
		}
		if !moved {
			return
		}
	}
}

// runEffect runs an effect for a task as stored.
func (a *App) runEffect(ctx context.Context, t store.Task, e flow.Effect) {
	switch e.Kind {
	case flow.Start:
		a.startQueued(ctx, t, e.Text)
	case flow.Check:
		if err := a.spawn(a.checkLog(t.ID, t.Round), "check", t.Label()); err != nil {
			a.fail(ctx, t, flow.ResumeCheck, "no verdict: the check did not start ("+err.Error()+"); csquad retry runs it again")
		}
	case flow.Send:
		eng, err := a.Engine(t.Engine)
		if err == nil {
			_, err = eng.Send(ctx, t.SessionID, e.Text)
		}
		if err != nil {
			a.fail(ctx, t, "", "message lost: csquad could not send to the worker ("+err.Error()+"); tell it with csquad send "+t.Label())
		}
	case flow.Land:
		a.land(ctx, t)
	}
}

// fail moves t (as stored) to needs_you with a note.
func (a *App) fail(ctx context.Context, t store.Task, resume, note string) {
	db, err := a.Store()
	if err != nil {
		return
	}
	n := t
	n.Phase, n.Step, n.Note = store.NeedsYou, resume, note
	if stored, err := db.Transition(ctx, t, n); err == nil {
		flow.Event(a.Home, t, stored, note)
	}
	a.warnf("csquad: %s needs you: %s\n", t.Label(), note)
}

// stopHook is the Stop hook command of a Claude task.
func (a *App) stopHook() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return assets.ProgramPath(exe, proc.Windows, a.Env.TildeHome()) + " hook --home " + assets.ShellPath(a.Home, proc.Windows) + " claude-stop"
}

// startQueued starts the session of a task whose dependencies are done.
func (a *App) startQueued(ctx context.Context, t store.Task, prompt string) {
	l, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return
	}
	defer l.Release()
	db, _ := a.Store()
	cur, err := db.Get(ctx, t.ID)
	if err != nil || !store.Same(t, cur) || cur.SessionID != "" {
		return
	}
	sid, err := a.startSession(ctx, cur, prompt, "")
	if err != nil {
		a.fail(ctx, cur, flow.ResumeWaiting, "did not start: "+firstLineOf(err.Error())+"; csquad retry starts it again")
		return
	}
	_ = db.SetSession(ctx, t.ID, sid)
}

// startSession starts t's session; fork names a session of the same engine
// to fork from ("" for a new one).
func (a *App) startSession(ctx context.Context, t store.Task, prompt, fork string) (string, error) {
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return "", err
	}
	started, err := eng.Start(ctx, engine.StartSpec{
		ID: t.ID, Title: t.Title, Cwd: t.Cwd, Model: t.Model, Prompt: prompt, Rules: t.WorkerRules, InPlace: t.InPlace,
		Args: t.EngineArgs, StopHook: a.stopHook(), Fork: fork,
	})
	if err != nil {
		return "", err
	}
	for _, n := range started.Notes {
		a.warnf("%s\n", n)
	}
	return started.SessionID, nil
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		rest := strings.TrimSpace(s[i+1:])
		s = strings.TrimSpace(s[:i])
		if rest != "" {
			s += " " + strings.Join(strings.Fields(rest), " ")
		}
	}
	return clip(s, 300)
}

// spawn starts a detached csquad (advance, check) with the allowlisted
// environment, its output appended to log. It is the only way csquad starts
// itself.
func (a *App) spawn(log string, args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(log); err == nil && st.Size() > 1<<20 {
		os.Rename(log, log+".1")
	}
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "--- %s csquad %s\n", a.Now().Format(time.RFC3339), strings.Join(args, " "))
	cmd := exec.Command(exe, args...)
	// CLAUDE_CONFIG_DIR only as the user set it (Base keeps it): set, Claude
	// Code reads its global config, with the workspace trust, from
	// $CLAUDE_CONFIG_DIR/.claude.json instead of ~/.claude.json, and a
	// queued task would not start ("Workspace not trusted").
	cmd.Env = a.Env.Base().With("CSQUAD_HOME", a.Home)
	cmd.Dir = a.Home
	cmd.Stdout, cmd.Stderr = f, f
	proc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Advance is the internal `csquad advance`: AdvanceAll, spawned by the
// hooks and the status line. With stopped (a Stop hook event) it first waits
// until the session no longer reads as working (the hook runs before the
// turn ends), then counts the turn as finished.
func (a *App) Advance(ctx context.Context, stopped string) error {
	if stopped == "" {
		l, err := flock.Try(a.AdvanceLock())
		if err != nil {
			return nil // another advance is at it
		}
		defer l.Release()
		a.AdvanceAll(ctx, nil)
		return nil
	}
	id, err := ParseLabel(stopped)
	if err != nil {
		return err
	}
	at := a.Now()
	db, err := a.Store()
	if err != nil {
		return err
	}
	deadline := at.Add(a.envMs("CSQUAD_STOP_WAIT_MS", 2*time.Minute))
	for {
		t, err := db.Get(ctx, id)
		if err != nil || t.SessionID == "" {
			break
		}
		live, err := a.observe(ctx, []store.Task{t})
		if err == nil && live[t.ID].State != engine.Working || !a.Now().Before(deadline) {
			break
		}
		time.Sleep(a.envMs("CSQUAD_STOP_POLL_MS", time.Second))
	}
	if l, err := flock.Wait(a.AdvanceLock(), time.Minute); err == nil {
		defer l.Release()
	}
	a.AdvanceAll(ctx, map[int]time.Time{id: at})
	return nil
}

// maybeAdvance spawns a detached advance when some task would move and no
// advance runs: the status line observes, it does not act.
func (a *App) maybeAdvance(ctx context.Context, tasks []store.Task, live map[int]engine.Live) {
	var dependents map[int][]int
	if db, err := a.Store(); err == nil {
		all, _ := db.List(ctx, store.Filter{})
		dependents = waitingOn(all)
	}
	for _, t := range tasks {
		if t.Phase != store.Waiting && !flow.Orchestrated(t) && !flow.Implicit(t, dependents[t.ID]) {
			continue
		}
		o := a.observation(ctx, t, live, nil)
		if flow.Next(t, o).Acts(t) {
			if !flock.Held(a.AdvanceLock()) {
				_ = a.spawn(filepath.Join(a.logsDir(), "advance.log"), "advance")
			}
			return
		}
	}
}

// Check is the internal `csquad check T12`: run the check of the task's
// current round once and apply its verdict.
func (a *App) Check(ctx context.Context, label string) error {
	id, err := ParseLabel(label)
	if err != nil {
		return err
	}
	db, err := a.Store()
	if err != nil {
		return err
	}
	t, err := db.Get(ctx, id)
	if err != nil || t.Phase != store.Checking {
		return err
	}
	round := t.Round
	// An observer probing the lock holds it a moment.
	l, err := flock.Wait(a.CheckLock(id, round), 2*time.Second)
	if err != nil {
		a.printf("the check of %s round %d is already running\n", t.Label(), round)
		return nil
	}
	defer l.Release()
	if t, err = db.Get(ctx, id); err != nil || t.Phase != store.Checking || t.Round != round {
		return err
	}
	v, ok := a.readVerdict(t)
	if !ok {
		spec, err := check.ParseSpec(t.CheckSpec)
		if err != nil {
			v = check.Verdict{Error: err.Error()}
		} else {
			a.printf("check %d of %d for %s: %s\n", round, t.MaxRounds, t.Name(), spec)
			req, err := a.checkRequest(ctx, t)
			if err != nil {
				v = check.Verdict{Error: err.Error()}
			} else {
				v = check.Run(ctx, spec, req)
			}
		}
		v.Round, v.For, v.At = round, t.UpdatedAt, a.Now().UnixMilli()
		b, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile(a.VerdictFile(id, round), append(b, '\n'), 0o600); err != nil {
			a.warnf("csquad: %v\n", err)
		}
	}
	a.printf("verdict: %s\n", verdictLine(v))
	o := flow.Obs{Now: a.Now(), Settle: a.settle(), Grace: a.grace(), Verdict: &v, Dependents: a.dependents(ctx, t.ID)}
	m := flow.Next(t, o)
	if _, _, err := a.mover().Apply(ctx, t, m); err != nil {
		a.printf("not applied (%v); the next csquad command applies it\n", err)
	}
	if m.Effect.Kind == flow.Land {
		// A merge that landed releases the tasks queued after it.
		a.AdvanceAll(ctx, nil)
	}
	return nil
}

func verdictLine(v check.Verdict) string {
	if v.Error != "" {
		return "none: " + v.Error
	}
	return fmt.Sprintf("%s · %s · %d blocking of %d findings", v.Verdict, v.Summary, v.Count(), len(v.Findings))
}

// checkRequest finds what a check of t looks at: the task's worktree and
// branch (or its checkout when in place), the target, the worker's final
// message and the previous round's findings.
func (a *App) checkRequest(ctx context.Context, t store.Task) (check.Request, error) {
	req := check.Request{ID: t.ID, Title: t.Title, Prompt: t.Prompt, Round: t.Round, Max: t.MaxRounds, Env: a.Env.Base(),
		Log: a.Stdout, Timeout: a.envMs("CSQUAD_CHECK_TIMEOUT_MS", 30*time.Minute), Poll: a.envMs("CSQUAD_CHECK_POLL_MS", 3*time.Second),
		Engine: a.Engine, Target: t.MergeInto}
	var l engine.Live
	if t.SessionID != "" {
		if eng, err := a.Engine(t.Engine); err == nil {
			if msgs, err := eng.Last(ctx, t.SessionID, 0); err == nil {
				req.Final = engine.LastAssistant(msgs)
			}
		}
		live, _ := a.observe(ctx, []store.Task{t})
		l = live[t.ID]
	}
	if t.Round > 1 {
		var prev check.Verdict
		if json.Unmarshal([]byte(t.LastCheck), &prev) == nil && prev.Verdict == "fail" {
			req.Previous = &prev
		}
	}
	if t.InPlace {
		req.Dir = t.Cwd
		return req, nil
	}
	p, err := a.inspect(t, t.MergeInto, l)
	if err != nil {
		return req, err
	}
	if req.Target == "" {
		req.Target = p.target
	}
	w, ok := taskWorktree(p, l)
	if !ok {
		return req, fmt.Errorf("no worktree of %s found (named t%d-… or the session's own) to check", t.Label(), t.ID)
	}
	req.Dir, req.Branch = w.Path, w.Branch
	return req, nil
}

// taskWorktree is the task's own worktree: the session's, else the only one
// named for it.
func taskWorktree(p plan, l engine.Live) (gitx.Worktree, bool) {
	for _, w := range p.worktrees {
		if l.Cwd != "" && sameDir(w.Path, l.Cwd) {
			return w, true
		}
	}
	if len(p.worktrees) == 1 {
		return p.worktrees[0], true
	}
	return gitx.Worktree{}, false
}

// land lands a task's merge: claimed with the task lock and a
// compare-and-set from the row as observed, then the rebase, the
// fast-forward, the verification and the cleanup; the result moves the
// task.
func (a *App) land(ctx context.Context, t store.Task) {
	l, err := flock.Try(a.TaskLock(t.ID))
	if err != nil {
		return
	}
	defer l.Release()
	db, _ := a.Store()
	cur, err := db.Get(ctx, t.ID)
	if err != nil || !store.Same(t, cur) || cur.Phase != store.Merging {
		return
	}
	res := a.landing(ctx, cur)
	o := flow.Obs{Now: a.Now(), Settle: a.settle(), Grace: a.grace(), Land: &res}
	stored, won, err := a.mover().Apply(ctx, cur, flow.Next(cur, o))
	if err != nil {
		a.warnf("csquad: %s: %v\n", t.Label(), err)
	}
	if won && stored.Phase == store.Done {
		a.removeFiles(t.ID, l)
	}
}

// landing rebases the task's branch onto the target in its worktree,
// fast-forwards the target, verifies the merge and cleans up as finish does.
// The target is --merge's, else (a merge for the tasks queued after it) the
// branch of the main checkout. A task in place has nothing to merge: it is
// done as finish does it.
func (a *App) landing(ctx context.Context, t store.Task) flow.Landing {
	res := flow.Landing{Target: t.MergeInto}
	problem := func(format string, args ...any) flow.Landing {
		res.Problem = fmt.Sprintf(format, args...)
		return res
	}
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return problem("%v", err)
	}
	var l engine.Live
	if t.SessionID != "" {
		live, err := a.observe(ctx, []store.Task{t})
		if err != nil {
			return problem("cannot observe the session: %v", err)
		}
		l = live[t.ID]
		if l.State == engine.Working || l.State == engine.NeedsInput {
			return problem("the session is %s", stateText(l.State))
		}
	}
	if t.InPlace {
		return a.landed(ctx, eng, t, l, plan{}, nil)
	}
	g := gitx.Git{Env: a.Env}
	p, err := a.inspect(t, t.MergeInto, l)
	if err != nil {
		return problem("%v", err)
	}
	if p.repo == "" {
		return problem("%s is not in a git repository", t.Cwd)
	}
	into := p.target
	res.Target = into
	for _, w := range p.worktrees {
		if dirty, err := g.Dirty(w.Path); err != nil || dirty != "" {
			return problem("worktree %s has uncommitted changes", w.Path)
		}
	}
	var unmerged []string
	for _, b := range p.branches {
		if b == into {
			return problem("branch %s is the target itself", b)
		}
		// A branch whose commits are all in the target by patch has
		// nothing to land; the cleanup deletes it.
		ok, _, _, err := g.Merged(p.repo, "refs/heads/"+b, "refs/heads/"+into)
		if err != nil {
			return problem("%v", err)
		}
		if !ok {
			unmerged = append(unmerged, b)
		}
	}
	switch {
	case len(p.branches) == 0:
		return problem("no branch of %s found (worktree-t%d-… or task/t%d-…)", t.Label(), t.ID, t.ID)
	case len(unmerged) > 1:
		return problem("several unmerged branches: %s", strings.Join(unmerged, ", "))
	}
	if len(unmerged) == 1 {
		b := unmerged[0]
		res.Branch = b
		var wt *gitx.Worktree
		for i := range p.worktrees {
			if p.worktrees[i].Branch == b {
				wt = &p.worktrees[i]
			}
		}
		if wt == nil {
			based, err := g.IsAncestor(p.repo, "refs/heads/"+into, "refs/heads/"+b)
			if err != nil || !based {
				return problem("branch %s is not based on %s and has no worktree to rebase in", b, into)
			}
		} else {
			res.Dir = wt.Path
			if out, err := g.Run(wt.Path, "rebase", into); err != nil {
				_, _ = g.Run(wt.Path, "rebase", "--abort")
				res.Conflict = strings.TrimSpace(out + "\n" + err.Error())
				return res
			}
		}
		// Fast-forward the target where it is checked out, else its ref.
		all, err := g.Worktrees(p.repo)
		if err != nil {
			return problem("%v", err)
		}
		var target *gitx.Worktree
		for i := range all {
			if all[i].Branch == into {
				target = &all[i]
			}
		}
		if target != nil {
			if _, err := g.Run(target.Path, "merge", "--ff-only", "-q", b); err != nil {
				return problem("fast-forward of %s in %s refused: %s", into, target.Path, firstLineOf(err.Error()))
			}
		} else if _, err := g.Run(p.repo, "fetch", "-q", ".", "refs/heads/"+b+":refs/heads/"+into); err != nil {
			return problem("fast-forward of %s refused: %s", into, firstLineOf(err.Error()))
		}
	} else {
		res.Branch = strings.Join(p.branches, ", ")
	}
	// The same verification and cleanup as finish.
	p, err = a.inspect(t, into, l)
	if err != nil {
		return problem("%v", err)
	}
	if len(p.problems) > 0 {
		return problem("unproven merge: %s", p.summary())
	}
	removed, _, err := a.clean(p, false)
	if err != nil {
		return problem("merged, but the cleanup stopped: %v", err)
	}
	done := a.landed(ctx, eng, t, l, p, removed)
	done.Branch, done.Dir = res.Branch, res.Dir
	return done
}

// landed ends a landing as finish ends: the worker's final message, then
// the session goes.
func (a *App) landed(ctx context.Context, eng engine.Engine, t store.Task, l engine.Live, p plan, removed []string) flow.Landing {
	final := ""
	if t.SessionID != "" {
		if msgs, err := eng.Last(ctx, t.SessionID, 0); err == nil {
			final = engine.LastAssistant(msgs)
		}
		if l.State != engine.Gone {
			if _, err := eng.Remove(ctx, t.SessionID, removed); err != nil {
				a.warnf("csquad: %s: the session is left: %v\n", t.Label(), err)
			}
		}
	}
	b, _ := json.Marshal(flow.Outcome{Target: p.target, Branches: p.branches, Final: final})
	return flow.Landing{Done: true, Outcome: string(b), Target: p.target}
}

// Retry resumes a task that needs you where it stopped.
func (a *App) Retry(ctx context.Context, label string, all bool) error {
	t, err := a.task(ctx, label, all, "retry")
	if err != nil {
		return err
	}
	m, err := flow.Retry(t)
	if err != nil {
		return err
	}
	if _, _, err := a.mover().Apply(ctx, t, m); err != nil {
		return fmt.Errorf("%s: %v", t.Label(), err)
	}
	a.printf("%s · %s\n", t.Name(), m.Reason)
	if m.Effect.Kind == flow.Land {
		// A merge that landed releases the tasks queued after it.
		a.AdvanceAll(ctx, nil)
	}
	return nil
}

// Hook is a Stop hook entry point (claude-stop, codex-stop): it reads the
// hook's JSON, and for a task's session spawns `csquad advance --stopped
// T12` and returns at once. It never fails the session's turn.
func (a *App) Hook(ctx context.Context, which string) error {
	var engineName string
	switch which {
	case "claude-stop":
		engineName = "claude"
	case "codex-stop":
		engineName = "codex"
	default:
		return fmt.Errorf("unknown hook %q (claude-stop, codex-stop)", which)
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if a.Stdin != nil {
		_ = json.NewDecoder(a.Stdin).Decode(&in)
	}
	if in.SessionID == "" {
		return nil
	}
	if _, err := os.Stat(a.DBFile()); err != nil {
		return nil
	}
	db, err := a.Store()
	if err != nil {
		return nil
	}
	tasks, err := db.List(ctx, store.Filter{})
	if err != nil {
		return nil
	}
	dependents := waitingOn(tasks)
	for _, t := range tasks {
		if t.Engine != engineName || t.SessionID == "" || !flow.Orchestrated(t) && !flow.Implicit(t, dependents[t.ID]) {
			continue
		}
		if t.SessionID == in.SessionID || strings.HasPrefix(in.SessionID, t.SessionID) {
			_ = a.spawn(filepath.Join(a.logsDir(), "advance.log"), "advance", "--stopped", t.Label())
			return nil
		}
	}
	return nil
}
