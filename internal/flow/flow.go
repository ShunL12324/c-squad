// Package flow is csquad's task flow: dependencies, checks, fix rounds and
// merging. Next is pure: from a task and what was observed about it, the
// row it moves to and the effect to run. Apply writes that row with a
// compare-and-set and runs the effect only when its own write won, so the
// hook, the status line and concurrent commands never act twice.
package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/check"
	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/store"
)

// Steps inside a phase (store.Task.Step).
const (
	Asked = "asked" // merging: the worker was asked to rebase

	// needs_you: what retry resumes.
	ResumeWaiting = "waiting" // start again (no session)
	ResumeRound   = "round"   // one more fix round
	ResumeCheck   = "check"   // run the check again
	ResumeMerge   = "merge"   // land the merge again
)

// Effects.
const (
	Start = "start" // start the session with Effect.Text as its prompt
	Check = "check" // spawn `csquad check T12`
	Send  = "send"  // send Effect.Text to the worker
	Land  = "land"  // rebase, fast-forward the target, verify, clean up
)

// Effect is what to do once the row is written.
type Effect struct {
	Kind string
	Text string
}

// Landing is the result of landing a merge.
type Landing struct {
	Done     bool
	Outcome  string // done: JSON {target, branches, final}
	Conflict string // the rebase stopped on conflicts (git's message)
	Problem  string // anything else that stops it: dirty, refused, unproven
	Branch   string
	Dir      string // the task's worktree
	Target   string
}

// Obs is what was observed about a task.
type Obs struct {
	Now      time.Time
	Settle   time.Duration // an idle session without a hook event counts as finished after this
	Grace    time.Duration // a free lock this long without a result means the process died
	Observed bool          // the engine answered for the task's session
	Live     engine.Live
	Stopped  time.Time // a Stop hook event for the session
	Replied  bool      // the conversation shows the worker's answer to csquad's message, after the task entered its step
	// Deps are the tasks of t.After as stored; a missing one does not exist.
	Deps map[int]store.Task
	// Busy: a process holds the task's lock (starting, landing) or the
	// current round's check lock.
	Busy bool
	// Quiet is the check's last sign of life (its log's last write).
	Quiet   time.Time
	Verdict *check.Verdict // the current round's verdict, written for this version of the task
	Land    *Landing
}

// Move is what Next decided.
type Move struct {
	Next   store.Task
	Effect Effect
	Reason string
}

// Changed reports whether the move writes the row.
func (m Move) Changed(t store.Task) bool {
	n := m.Next
	return n.Phase != t.Phase || n.Step != t.Step || n.Round != t.Round || n.MaxRounds != t.MaxRounds ||
		n.LastCheck != t.LastCheck || n.Note != t.Note || n.Outcome != t.Outcome
}

// Acts reports whether the move does anything.
func (m Move) Acts(t store.Task) bool { return m.Changed(t) || m.Effect.Kind != "" }

// Orchestrated reports whether a task has a flow beyond 1.x's: a check or
// a merge. Other tasks keep their phase until finish or cancel.
func Orchestrated(t store.Task) bool { return t.CheckSpec != "" || t.MergeInto != "" }

func entered(t store.Task) time.Time { return time.UnixMilli(t.UpdatedAt) }

// finished reports whether the worker's turn ended: the session reads idle
// (or done) and either a Stop hook event came after the task entered its
// step, or, without one, the task has been in it for Settle. In fixing and
// merging (the worker was messaged) only the hook event or the worker's
// reply counts: right after a message the session still reads idle.
func finished(t store.Task, o Obs) bool {
	if !o.Observed || (o.Live.State != engine.Idle && o.Live.State != engine.Done) {
		return false
	}
	if !o.Stopped.IsZero() && o.Stopped.After(entered(t)) {
		return true
	}
	if t.Phase == store.Fixing || t.Phase == store.Merging {
		return o.Replied
	}
	return o.Now.Sub(entered(t)) >= o.Settle
}

func to(t store.Task, phase, step string) store.Task {
	n := t
	n.Phase, n.Step = phase, step
	if phase != store.NeedsYou {
		n.Note = ""
	}
	return n
}

func needsYou(t store.Task, resume, note string) Move {
	n := to(t, store.NeedsYou, resume)
	n.Note = note
	return Move{Next: n, Reason: note}
}

// Next decides where t goes from what was observed. It is pure.
func Next(t store.Task, o Obs) Move {
	stay := Move{Next: t}
	switch t.Phase {
	case store.Waiting:
		return nextWaiting(t, o)
	case store.Running, store.Fixing:
		if t.SessionID == "" {
			if t.Phase == store.Running && len(t.After) > 0 && !o.Busy && o.Now.Sub(entered(t)) >= o.Grace {
				return needsYou(t, ResumeWaiting, "did not start: the process starting the session stopped; csquad retry starts it again")
			}
			return stay
		}
		if !Orchestrated(t) {
			return stay
		}
		if m, ok := lost(t, o); ok {
			return m
		}
		if !finished(t, o) {
			return stay
		}
		return afterWork(t)
	case store.Checking:
		if o.Verdict != nil {
			return verdict(t, *o.Verdict, o)
		}
		quiet := entered(t)
		if o.Quiet.After(quiet) {
			quiet = o.Quiet
		}
		if !o.Busy && o.Now.Sub(quiet) >= o.Grace {
			return needsYou(t, ResumeCheck, "no verdict: the check stopped without one; csquad retry runs it again")
		}
		return stay
	case store.Merging:
		if o.Land != nil {
			return landed(t, *o.Land)
		}
		if m, ok := lost(t, o); ok {
			return m
		}
		if t.Step == Asked {
			if finished(t, o) {
				return Move{Next: t, Effect: Effect{Kind: Land}, Reason: "the worker stopped after the rebase request"}
			}
			return stay
		}
		if !o.Busy && o.Now.Sub(entered(t)) >= o.Grace {
			return Move{Next: t, Effect: Effect{Kind: Land}, Reason: "landing again: the last attempt stopped"}
		}
		return stay
	case store.NeedsYou, store.Failed:
		// Back to work only when the session works again (the user took
		// over), not when it merely reads as needing input.
		if t.SessionID != "" && o.Observed && o.Live.State == engine.Working {
			n := to(t, store.Running, "")
			return Move{Next: n, Reason: "the session works again"}
		}
	}
	return stay
}

// lost handles a session that failed or disappeared under a running flow.
// While the task's lock is held its session may be going away on purpose
// (landing removes it before the task is done).
func lost(t store.Task, o Obs) (Move, bool) {
	if !o.Observed || o.Busy {
		return Move{}, false
	}
	switch o.Live.State {
	case engine.Failed:
		n := to(t, store.Failed, "")
		n.Note = "the session failed"
		return Move{Next: n, Reason: n.Note}, true
	case engine.Gone:
		return needsYou(t, "", "session gone: it was removed outside csquad"), true
	}
	return Move{}, false
}

// afterWork is where a task goes when its worker finished: the check, else
// the merge.
func afterWork(t store.Task) Move {
	switch {
	case t.CheckSpec != "":
		n := to(t, store.Checking, "")
		n.Round = max(n.Round, 1)
		return Move{Next: n, Effect: Effect{Kind: Check}, Reason: fmt.Sprintf("the worker stopped; check %d of %d", n.Round, n.MaxRounds)}
	case t.MergeInto != "":
		return Move{Next: to(t, store.Merging, ""), Effect: Effect{Kind: Land}, Reason: "the worker stopped; merging into " + t.MergeInto}
	}
	return Move{Next: t}
}

func nextWaiting(t store.Task, o Obs) Move {
	var missing, open []string
	for _, id := range t.After {
		d, ok := o.Deps[id]
		switch {
		case !ok:
			missing = append(missing, fmt.Sprintf("T%d does not exist", id))
		case d.Phase == store.Cancelled:
			missing = append(missing, fmt.Sprintf("T%d was cancelled", id))
		case d.Phase != store.Done:
			open = append(open, d.Label())
		}
	}
	if len(missing) > 0 {
		return needsYou(t, ResumeWaiting, "dependency gone: "+strings.Join(missing, ", ")+"; csquad retry waits again, csquad cancel drops this task")
	}
	if len(open) > 0 {
		return Move{Next: t}
	}
	n := to(t, store.Running, "")
	return Move{Next: n, Effect: Effect{Kind: Start, Text: StartPrompt(t, o.Deps)}, Reason: "its dependencies are done"}
}

// Outcome is what a done task left, kept in its outcome column.
type Outcome struct {
	Target   string   `json:"target"`
	Branches []string `json:"branches"`
	Final    string   `json:"final"`
}

// StartPrompt is a queued task's prompt with what its dependencies left.
func StartPrompt(t store.Task, deps map[int]store.Task) string {
	if len(t.After) == 0 {
		return t.Prompt
	}
	var b strings.Builder
	b.WriteString(t.Prompt)
	var ids []string
	for _, id := range t.After {
		ids = append(ids, "T"+strconv.Itoa(id))
	}
	fmt.Fprintf(&b, "\n\n---\nThis task was queued to start after %s, which are done. What they left:\n", strings.Join(ids, ", "))
	for _, id := range t.After {
		d := deps[id]
		var oc Outcome
		_ = json.Unmarshal([]byte(d.Outcome), &oc)
		fmt.Fprintf(&b, "\n%s", d.Name())
		switch {
		case len(oc.Branches) > 0 && oc.Target != "":
			fmt.Fprintf(&b, ": merged %s into %s.", strings.Join(oc.Branches, ", "), oc.Target)
		case oc.Target != "":
			fmt.Fprintf(&b, ": finished; its work is in %s.", oc.Target)
		default:
			b.WriteString(": finished.")
		}
		if f := strings.TrimSpace(oc.Final); f != "" {
			fmt.Fprintf(&b, " Its final message:\n<<<\n%s\n>>>\n", f)
		} else {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// verdict applies a check's verdict.
func verdict(t store.Task, v check.Verdict, o Obs) Move {
	var prev check.Verdict
	_ = json.Unmarshal([]byte(t.LastCheck), &prev)
	v.Round, v.Blocking = t.Round, nil
	if v.At == 0 {
		v.At = o.Now.UnixMilli()
	}
	if v.Error == "" {
		// The blocking counts of earlier rounds, then this one.
		for i, n := range prev.Blocking {
			if i < t.Round-1 {
				v.Blocking = append(v.Blocking, n)
			}
		}
		v.Blocking = append(v.Blocking, v.Count())
	} else {
		v.Blocking = prev.Blocking
	}
	b, _ := json.Marshal(v)
	n := t
	n.LastCheck = string(b)
	switch {
	case v.Error != "":
		return needsYou(n, ResumeCheck, "no verdict: "+v.Error+"; csquad retry runs the check again")
	case v.Pass():
		if t.MergeInto != "" {
			m := to(n, store.Merging, "")
			return Move{Next: m, Effect: Effect{Kind: Land}, Reason: fmt.Sprintf("check %d passed; merging into %s", t.Round, t.MergeInto)}
		}
		return Move{Next: to(n, store.Ready, ""), Reason: fmt.Sprintf("check %d passed", t.Round)}
	case t.Round >= t.MaxRounds:
		return needsYou(n, ResumeRound, fmt.Sprintf("max rounds: %d of %d checks did not pass (%s); csquad retry runs one more round", t.Round, t.MaxRounds, oneLine(v.Summary)))
	case stuck(t, v):
		return needsYou(n, ResumeRound, fmt.Sprintf("not converging: blocking findings %s over the last rounds; csquad retry runs one more round", counts(v.Blocking)))
	}
	f := to(n, store.Fixing, "")
	f.Round = t.Round + 1
	return Move{Next: f, Effect: Effect{Kind: Send, Text: FixMessage(t, v)}, Reason: fmt.Sprintf("check %d did not pass; findings sent", t.Round)}
}

// stuck is the convergence guard: a reviewer's blocking findings did not
// shrink for two rounds in a row.
func stuck(t store.Task, v check.Verdict) bool {
	spec, err := check.ParseSpec(t.CheckSpec)
	if err != nil || !spec.Reviewer() || len(v.Blocking) < 3 {
		return false
	}
	h := v.Blocking[len(v.Blocking)-3:]
	return h[1] >= h[0] && h[2] >= h[1]
}

func counts(h []int) string {
	if len(h) > 3 {
		h = h[len(h)-3:]
	}
	var s []string
	for _, n := range h {
		s = append(s, strconv.Itoa(n))
	}
	return strings.Join(s, " → ")
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:119]) + "…"
	}
	return s
}

// FixMessage is what the worker gets when a check fails.
func FixMessage(t store.Task, v check.Verdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "csquad: check %d of %d (%s) did not pass.", t.Round, t.MaxRounds, t.CheckSpec)
	if s := strings.TrimSpace(v.Summary); s != "" {
		b.WriteString(" " + s)
	}
	b.WriteString("\n\nFindings:\n")
	for _, f := range v.Findings {
		b.WriteString("- " + strings.ReplaceAll(check.FormatFinding(f), "\n", "\n  ") + "\n")
	}
	b.WriteString("\nFix them in your worktree, re-run the relevant tests, commit, and end your turn with a short result. csquad checks the work again when you stop.")
	return b.String()
}

// MergeRequest is what the worker gets when its branch does not rebase
// cleanly onto the target.
func MergeRequest(l Landing) string {
	return fmt.Sprintf("csquad: the work is accepted, but %s does not rebase cleanly onto %s. In your worktree %s run `git rebase %s`, resolve the conflicts, "+
		"re-run the relevant tests and complete the rebase, so the branch is committed and clean. Do not merge into %s and do not touch the main checkout: "+
		"csquad fast-forwards %s itself when you stop.\n\nWhat git said:\n%s",
		l.Branch, l.Target, l.Dir, l.Target, l.Target, l.Target, strings.TrimSpace(l.Conflict))
}

func landed(t store.Task, l Landing) Move {
	switch {
	case l.Done:
		n := to(t, store.Done, "")
		n.Outcome = l.Outcome
		return Move{Next: n, Reason: fmt.Sprintf("merged %s into %s", l.Branch, l.Target)}
	case l.Conflict != "" && t.Step != Asked:
		return Move{Next: to(t, store.Merging, Asked), Effect: Effect{Kind: Send, Text: MergeRequest(l)}, Reason: "rebase conflict; the worker was asked to rebase"}
	case l.Conflict != "":
		return needsYou(t, ResumeMerge, fmt.Sprintf("merge conflict: %s still does not rebase onto %s; csquad retry tries again", l.Branch, l.Target))
	}
	return needsYou(t, ResumeMerge, "merge stopped: "+l.Problem+"; csquad retry tries again")
}

// Retry resumes a needs_you task where it stopped.
func Retry(t store.Task) (Move, error) {
	if t.Phase != store.NeedsYou {
		return Move{}, fmt.Errorf("%s is %s; retry resumes a task that needs you", t.Label(), strings.ReplaceAll(t.Phase, "_", " "))
	}
	switch t.Step {
	case ResumeWaiting:
		return Move{Next: to(t, store.Waiting, ""), Reason: "retry: waiting again"}, nil
	case ResumeCheck:
		return Move{Next: to(t, store.Checking, ""), Effect: Effect{Kind: Check}, Reason: fmt.Sprintf("retry: check %d again", t.Round)}, nil
	case ResumeMerge:
		return Move{Next: to(t, store.Merging, ""), Effect: Effect{Kind: Land}, Reason: "retry: merging again"}, nil
	case ResumeRound:
		var v check.Verdict
		_ = json.Unmarshal([]byte(t.LastCheck), &v)
		n := to(t, store.Fixing, "")
		n.MaxRounds = t.MaxRounds + 1
		n.Round = t.Round + 1
		cur := t
		cur.MaxRounds = n.MaxRounds
		return Move{Next: n, Effect: Effect{Kind: Send, Text: FixMessage(cur, v)}, Reason: fmt.Sprintf("retry: one more round (%d of %d)", n.Round, n.MaxRounds)}, nil
	}
	return Move{}, fmt.Errorf("%s cannot resume by itself (%s); attach to the session or cancel it", t.Label(), t.Note)
}

// Mover writes moves and runs their effects.
type Mover struct {
	DB   *store.Store
	Home string
	// Run runs an effect for the task as stored.
	Run func(ctx context.Context, t store.Task, e Effect)
}

// Apply writes m over t (compare-and-set) and runs the effect only when the
// write won. A move that only acts (Land again) runs its effect, which
// claims the task itself. It returns the row as stored and whether this
// process moved it.
func (mv *Mover) Apply(ctx context.Context, t store.Task, m Move) (store.Task, bool, error) {
	if !m.Acts(t) {
		return t, false, nil
	}
	stored := t
	if m.Changed(t) {
		var err error
		if stored, err = mv.DB.Transition(ctx, t, m.Next); err != nil {
			return t, false, err
		}
		if stored.Phase != t.Phase || stored.Step != t.Step {
			Event(mv.Home, t, stored, m.Reason)
		}
	}
	if m.Effect.Kind != "" && mv.Run != nil {
		mv.Run(ctx, stored, m.Effect)
	}
	return stored, true, nil
}

// EventsFile is the event log: one JSON line per phase change.
func EventsFile(home string) string { return filepath.Join(home, "logs", "events.jsonl") }

// Event appends a phase change to the event log.
func Event(home string, from, to store.Task, reason string) {
	e := map[string]any{"task": from.Label(), "from": phaseStep(from), "to": phaseStep(to), "reason": reason,
		"time": time.UnixMilli(to.UpdatedAt).UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(e)
	file := EventsFile(home)
	if os.MkdirAll(filepath.Dir(file), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
	f.Close()
}

func phaseStep(t store.Task) string {
	if t.Step != "" && t.Phase == store.Merging {
		return t.Phase + "/" + t.Step
	}
	return t.Phase
}
