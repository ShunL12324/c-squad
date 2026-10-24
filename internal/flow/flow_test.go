package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/check"
	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/store"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// task is a task that entered its phase at t0.
func task(phase string, mod ...func(*store.Task)) store.Task {
	t := store.Task{ID: 12, Title: "fix it", Prompt: "do it", Engine: "claude", SessionID: "abc", Phase: phase, MaxRounds: 3,
		CheckSpec: "codex", UpdatedAt: t0.UnixMilli()}
	for _, m := range mod {
		m(&t)
	}
	return t
}

func obs(after time.Duration, state string, mod ...func(*Obs)) Obs {
	o := Obs{Now: t0.Add(after), Settle: 10 * time.Second, Grace: 30 * time.Second, Observed: state != "", Live: engine.Live{State: state}}
	for _, m := range mod {
		m(&o)
	}
	return o
}

func stopped(at time.Duration) func(*Obs) { return func(o *Obs) { o.Stopped = t0.Add(at) } }
func replied(o *Obs)                      { o.Replied = true }
func busy(o *Obs)                         { o.Busy = true }
func withVerdict(v check.Verdict) func(*Obs) {
	return func(o *Obs) { o.Verdict = &v }
}
func withLand(l Landing) func(*Obs) { return func(o *Obs) { o.Land = &l } }
func round(n int) func(*store.Task) { return func(t *store.Task) { t.Round = n } }
func merge(t *store.Task)           { t.MergeInto = "main" }
func noCheck(t *store.Task)         { t.CheckSpec = "" }
func step(s string) func(*store.Task) {
	return func(t *store.Task) { t.Step = s }
}

func fail(n int) check.Verdict {
	v := check.Verdict{Verdict: "fail", Summary: "bad"}
	for i := 0; i < n; i++ {
		v.Findings = append(v.Findings, check.Finding{Severity: "blocking", File: "a.go", Summary: "broken"})
	}
	return v
}

var pass = check.Verdict{Verdict: "pass", Summary: "good"}

func TestNext(t *testing.T) {
	cases := []struct {
		name   string
		t      store.Task
		o      Obs
		phase  string
		step   string
		effect string
		round  int
	}{
		// running: finished = settled idle, or a hook event
		{"running working", task(store.Running), obs(time.Hour, engine.Working), store.Running, "", "", 0},
		{"running idle, not settled", task(store.Running), obs(5*time.Second, engine.Idle), store.Running, "", "", 0},
		{"running idle settled → check", task(store.Running), obs(10*time.Second, engine.Idle), store.Checking, "", Check, 1},
		{"running done settled → check", task(store.Running), obs(time.Minute, engine.Done), store.Checking, "", Check, 1},
		{"running hook event at once", task(store.Running), obs(time.Second, engine.Idle, stopped(500*time.Millisecond)), store.Checking, "", Check, 1},
		{"hook event but still working", task(store.Running), obs(time.Minute, engine.Working, stopped(time.Second)), store.Running, "", "", 0},
		{"needs input is not finished", task(store.Running), obs(time.Hour, engine.NeedsInput), store.Running, "", "", 0},
		{"not observed", task(store.Running), obs(time.Hour, ""), store.Running, "", "", 0},
		{"no check, merge → merging", task(store.Running, noCheck, merge), obs(time.Minute, engine.Idle), store.Merging, "", Land, 0},
		{"plain task stays", task(store.Running, noCheck), obs(time.Hour, engine.Idle), store.Running, "", "", 0},
		{"plain task failed stays (1.x)", task(store.Running, noCheck), obs(time.Hour, engine.Failed), store.Running, "", "", 0},
		{"failed session", task(store.Running), obs(time.Minute, engine.Failed), store.Failed, "", "", 0},
		{"gone session", task(store.Running), obs(time.Minute, engine.Gone), store.NeedsYou, "", "", 0},
		{"gone while busy (landing)", task(store.Merging, merge), obs(time.Minute, engine.Gone, busy), store.Merging, "", "", 0},
		{"queued start died", task(store.Running, func(t *store.Task) { t.SessionID, t.After = "", []int{3} }), obs(time.Minute, ""), store.NeedsYou, ResumeWaiting, "", 0},
		{"queued start in progress", task(store.Running, func(t *store.Task) { t.SessionID, t.After = "", []int{3} }), obs(time.Minute, "", busy), store.Running, "", "", 0},
		// fixing: only a hook event or the worker's reply
		{"fixing idle settled is not finished", task(store.Fixing, round(2)), obs(time.Hour, engine.Idle), store.Fixing, "", "", 2},
		{"fixing old hook event", task(store.Fixing, round(2)), obs(time.Hour, engine.Idle, stopped(-time.Second)), store.Fixing, "", "", 2},
		{"fixing hook event", task(store.Fixing, round(2)), obs(time.Minute, engine.Idle, stopped(30*time.Second)), store.Checking, "", Check, 2},
		{"fixing reply", task(store.Fixing, round(2)), obs(time.Minute, engine.Idle, replied), store.Checking, "", Check, 2},
		{"fixing reply but working", task(store.Fixing, round(2)), obs(time.Minute, engine.Working, replied), store.Fixing, "", "", 2},
		// checking
		{"checking, check running", task(store.Checking, round(1)), obs(time.Hour, "", busy), store.Checking, "", "", 1},
		{"checking, lock free briefly", task(store.Checking, round(1)), obs(20*time.Second, ""), store.Checking, "", "", 1},
		{"checking, lock free 30 s", task(store.Checking, round(1)), obs(30*time.Second, ""), store.NeedsYou, ResumeCheck, "", 1},
		{"checking, log written lately", task(store.Checking, round(1)), obs(time.Hour, "", func(o *Obs) { o.Quiet = t0.Add(time.Hour - 10*time.Second) }), store.Checking, "", "", 1},
		{"pass → ready", task(store.Checking, round(1)), obs(time.Minute, "", withVerdict(pass)), store.Ready, "", "", 1},
		{"pass → merging", task(store.Checking, round(1), merge), obs(time.Minute, "", withVerdict(pass)), store.Merging, "", Land, 1},
		{"fail → fixing", task(store.Checking, round(1)), obs(time.Minute, "", withVerdict(fail(2))), store.Fixing, "", Send, 2},
		{"fail at max", task(store.Checking, round(3)), obs(time.Minute, "", withVerdict(fail(1))), store.NeedsYou, ResumeRound, "", 3},
		{"no verdict", task(store.Checking, round(1)), obs(time.Minute, "", withVerdict(check.Verdict{Error: "timed out"})), store.NeedsYou, ResumeCheck, "", 1},
		// merging
		{"landed", task(store.Merging, merge), obs(time.Second, "", withLand(Landing{Done: true, Outcome: "{}"})), store.Done, "", "", 0},
		{"conflict → ask the worker", task(store.Merging, merge), obs(time.Second, "", withLand(Landing{Conflict: "CONFLICT a.go", Branch: "b", Target: "main"})), store.Merging, Asked, Send, 0},
		{"conflict again → needs you", task(store.Merging, merge, step(Asked)), obs(time.Second, "", withLand(Landing{Conflict: "CONFLICT", Branch: "b", Target: "main"})), store.NeedsYou, ResumeMerge, "", 0},
		{"unproven → needs you", task(store.Merging, merge), obs(time.Second, "", withLand(Landing{Problem: "unproven merge: x"})), store.NeedsYou, ResumeMerge, "", 0},
		{"asked, worker replied → land", task(store.Merging, merge, step(Asked)), obs(time.Minute, engine.Idle, replied), store.Merging, Asked, Land, 0},
		{"asked, settled idle is not enough", task(store.Merging, merge, step(Asked)), obs(time.Hour, engine.Idle), store.Merging, Asked, "", 0},
		{"landing stopped → land again", task(store.Merging, merge), obs(time.Minute, engine.Idle), store.Merging, "", Land, 0},
		{"landing in progress", task(store.Merging, merge), obs(time.Minute, engine.Idle, busy), store.Merging, "", "", 0},
		// needs you / failed
		{"needs you, needs input stays", task(store.NeedsYou, step(ResumeRound)), obs(time.Minute, engine.NeedsInput), store.NeedsYou, ResumeRound, "", 0},
		{"needs you, works again", task(store.NeedsYou, step(ResumeRound)), obs(time.Minute, engine.Working), store.Running, "", "", 0},
		{"failed, works again", task(store.Failed), obs(time.Minute, engine.Working), store.Running, "", "", 0},
		{"ready stays", task(store.Ready), obs(time.Hour, engine.Idle), store.Ready, "", "", 0},
	}
	for _, c := range cases {
		m := Next(c.t, c.o)
		if m.Next.Phase != c.phase || m.Next.Step != c.step || m.Effect.Kind != c.effect || m.Next.Round != c.round {
			t.Errorf("%s: got %s/%s effect %q round %d, want %s/%s effect %q round %d (%s)", c.name,
				m.Next.Phase, m.Next.Step, m.Effect.Kind, m.Next.Round, c.phase, c.step, c.effect, c.round, m.Reason)
		}
		if (m.Next.Phase != c.t.Phase || m.Effect.Kind != "") && m.Reason == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
}

func TestNextIsPure(t *testing.T) {
	tk := task(store.Checking, round(1))
	o := obs(time.Minute, "", withVerdict(fail(1)))
	a, b := Next(tk, o), Next(tk, o)
	if a.Changed(b.Next) || a.Effect != b.Effect || tk.Phase != store.Checking {
		t.Error("Next must not depend on anything but its arguments")
	}
}

// Four movers holding the same snapshot: one write wins, one effect runs.
func TestApplyRunsTheEffectOnlyForTheWinner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "csquad.db")
	first, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	snap, _ := first.Insert(context.Background(), store.Task{Title: "x", Cwd: "/", Engine: "claude", Prompt: "p", Phase: store.Running, SessionID: "abc", CheckSpec: "cmd:true"})
	var mu sync.Mutex
	effects, wins := 0, 0
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mv := &Mover{DB: db, Home: dir, Run: func(context.Context, store.Task, Effect) {
			mu.Lock()
			effects++
			mu.Unlock()
		}}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			m := Next(snap, Obs{Now: time.UnixMilli(snap.UpdatedAt).Add(time.Minute), Settle: time.Second, Observed: true, Live: engine.Live{State: engine.Idle}})
			if _, won, _ := mv.Apply(context.Background(), snap, m); won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if effects != 1 || wins != 1 {
		t.Errorf("effects %d, wins %d", effects, wins)
	}
	b, _ := os.ReadFile(EventsFile(dir))
	if strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), `"from":"running","reason":"the worker stopped; check 1 of 3","task":"T1","time":`) {
		t.Errorf("events:\n%s", b)
	}
}

func TestFindingsGoToTheWorker(t *testing.T) {
	m := Next(task(store.Checking, round(1)), obs(time.Minute, "", withVerdict(fail(2))))
	if !strings.HasPrefix(m.Effect.Text, "csquad: check 1 of 3 (codex) did not pass. bad\n\nFindings:\n- [blocking] a.go: broken\n- [blocking] a.go: broken\n") {
		t.Errorf("%q", m.Effect.Text)
	}
	var v check.Verdict
	json.Unmarshal([]byte(m.Next.LastCheck), &v)
	if v.Round != 1 || len(v.Blocking) != 1 || v.Blocking[0] != 2 || v.At == 0 {
		t.Errorf("last_check %s", m.Next.LastCheck)
	}
}

// The convergence guard: a reviewer's blocking findings that do not shrink
// for two rounds stop the loop, before max rounds.
func TestConvergenceGuard(t *testing.T) {
	history := func(counts ...int) func(*store.Task) {
		return func(t *store.Task) {
			b, _ := json.Marshal(check.Verdict{Verdict: "fail", Blocking: counts})
			t.LastCheck = string(b)
			t.MaxRounds = 9
		}
	}
	m := Next(task(store.Checking, round(3), history(2, 2)), obs(time.Minute, "", withVerdict(fail(3))))
	if m.Next.Phase != store.NeedsYou || m.Next.Step != ResumeRound || !strings.HasPrefix(m.Next.Note, "not converging: blocking findings 2 → 2 → 3") {
		t.Errorf("%+v", m.Next)
	}
	if m := Next(task(store.Checking, round(3), history(3, 2)), obs(time.Minute, "", withVerdict(fail(2)))); m.Next.Phase != store.Fixing {
		t.Errorf("shrank once: %s %s", m.Next.Phase, m.Next.Note)
	}
	if m := Next(task(store.Checking, round(2), history(2)), obs(time.Minute, "", withVerdict(fail(2)))); m.Next.Phase != store.Fixing {
		t.Errorf("one round without shrinking: %s", m.Next.Phase)
	}
	cmd := func(t *store.Task) { t.CheckSpec = "cmd:make test" }
	if m := Next(task(store.Checking, round(3), history(1, 1), cmd), obs(time.Minute, "", withVerdict(fail(1)))); m.Next.Phase != store.Fixing {
		t.Errorf("a command check is not guarded: %s", m.Next.Phase)
	}
}

func TestWaitingStartsWhenAllAreDone(t *testing.T) {
	waiting := task(store.Waiting, func(t *store.Task) { t.SessionID, t.After = "", []int{3, 4} })
	dep := func(id int, phase, outcome string) store.Task {
		return store.Task{ID: id, Title: "dep", Phase: phase, Outcome: outcome}
	}
	o := obs(time.Minute, "", func(o *Obs) {
		o.Deps = map[int]store.Task{3: dep(3, store.Done, `{"target":"main","branches":["worktree-t3-a"],"final":"Added a."}`), 4: dep(4, store.Ready, "")}
	})
	if m := Next(waiting, o); m.Next.Phase != store.Waiting || m.Effect.Kind != "" {
		t.Errorf("ready does not release: %+v", m)
	}
	o.Deps[4] = dep(4, store.Done, `{"target":"main","branches":[],"final":""}`)
	m := Next(waiting, o)
	if m.Next.Phase != store.Running || m.Effect.Kind != Start {
		t.Fatalf("%+v", m)
	}
	want := "do it\n\n---\nThis task was queued to start after T3, T4, which are done. What they left:\n\nT3 · dep: merged worktree-t3-a into main. Its final message:\n<<<\nAdded a.\n>>>\n\nT4 · dep: finished; its work is in main.\n"
	if m.Effect.Text != want {
		t.Errorf("prompt:\n%q\nwant\n%q", m.Effect.Text, want)
	}
	o.Deps[4] = dep(4, store.Cancelled, "")
	if m := Next(waiting, o); m.Next.Phase != store.NeedsYou || m.Next.Step != ResumeWaiting || !strings.Contains(m.Next.Note, "T4 was cancelled") {
		t.Errorf("%+v", m.Next)
	}
	delete(o.Deps, 4)
	if m := Next(waiting, o); !strings.Contains(m.Next.Note, "T4 does not exist") {
		t.Errorf("%+v", m.Next)
	}
}

func TestRetry(t *testing.T) {
	for _, c := range []struct {
		step, phase, effect string
		round, max          int
	}{
		{ResumeWaiting, store.Waiting, "", 2, 3},
		{ResumeCheck, store.Checking, Check, 2, 3},
		{ResumeMerge, store.Merging, Land, 2, 3},
		{ResumeRound, store.Fixing, Send, 3, 4},
	} {
		tk := task(store.NeedsYou, round(2), step(c.step), func(t *store.Task) { t.Note = "x" })
		m, err := Retry(tk)
		if err != nil || m.Next.Phase != c.phase || m.Next.Step != "" || m.Effect.Kind != c.effect || m.Next.Round != c.round || m.Next.MaxRounds != c.max || m.Next.Note != "" {
			t.Errorf("%s: %+v %v", c.step, m, err)
		}
	}
	if m, _ := Retry(task(store.NeedsYou, round(3), step(ResumeRound))); !strings.HasPrefix(m.Effect.Text, "csquad: check 3 of 4 (codex) did not pass.") {
		t.Errorf("%q", m.Effect.Text)
	}
	if _, err := Retry(task(store.Running)); err == nil {
		t.Error("retry of a running task")
	}
	if _, err := Retry(task(store.NeedsYou)); err == nil {
		t.Error("retry without a step to resume")
	}
}

func TestReplyIsNeededAfterAMergeRequest(t *testing.T) {
	m := Next(task(store.Merging, merge), obs(time.Second, "", withLand(Landing{Conflict: "CONFLICT (content): a.go", Branch: "worktree-t12-x", Dir: "/w", Target: "main"})))
	if !strings.Contains(m.Effect.Text, "In your worktree /w run `git rebase main`") || !strings.Contains(m.Effect.Text, "CONFLICT (content): a.go") {
		t.Errorf("%q", m.Effect.Text)
	}
}
