package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/check"
	"github.com/ShunL12324/c-squad/internal/engine/claude"
	"github.com/ShunL12324/c-squad/internal/flock"
	"github.com/ShunL12324/c-squad/internal/flow"
	"github.com/ShunL12324/c-squad/internal/gitx"
	"github.com/ShunL12324/c-squad/internal/store"
)

// LaunchArgs are launch's flags.
type LaunchArgs struct {
	Title         string
	Cwd           string
	Engine        string
	Model         string
	Isolation     string // "" | worktree | none
	PromptFile    string
	RmPrompt      bool
	NoWorkerRules bool
	EngineArgs    []string // after `--`
	After         string   // "T3,T4"
	Check         string   // codex[:model] | claude[:model] | cmd:<command>
	Max           int      // 0 = the default (3)
	Merge         bool
	MergeInto     string // "" with Merge = the main checkout's branch
	Fork          bool   // start as a fork of the dispatching session
}

// forkEnv names, per engine, the variable that holds the dispatching
// session's full id.
var forkEnv = map[string]string{"claude": "CLAUDE_CODE_SESSION_ID", "codex": "CODEX_THREAD_ID"}

// forkSource is the session --fork starts from: the dispatching session,
// which must be of the task's engine.
func (a *App) forkSource(engineName string) (string, error) {
	name, ok := forkEnv[engineName]
	if !ok {
		return "", fmt.Errorf("--fork works with the claude and codex engines")
	}
	id := a.Env.Get(name)
	if id == "" {
		for other, v := range forkEnv {
			if other != engineName && a.Env.Get(v) != "" {
				return "", fmt.Errorf("--fork forks the dispatching session, which is a %s session, and a fork stays in its engine: use --engine %s", other, other)
			}
		}
		return "", fmt.Errorf("--fork needs the dispatching session's id, and %s is not set: run launch from a %s session, or leave out --fork", name, engineName)
	}
	if engineName == "claude" && !claude.FullID(id) {
		return "", fmt.Errorf("--fork needs the full session id; %s is %q", name, id)
	}
	return id, nil
}

// parseAfter reads "T3,T4" into ids of existing tasks.
func (a *App) parseAfter(ctx context.Context, v string) ([]int, error) {
	db, err := a.Store()
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		id, err := ParseLabel(part)
		if err != nil {
			return nil, fmt.Errorf("--after: %v", err)
		}
		t, err := db.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("--after: T%d is not a csquad task (only existing tasks can be waited for)", id)
		}
		if t.Phase == store.Cancelled {
			return nil, fmt.Errorf("--after: %s was cancelled", t.Label())
		}
		if !containsInt(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--after needs task ids, e.g. --after T3,T4")
	}
	return ids, nil
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// Launch starts a task: the next number, a row in the table, then the
// session. A failed start deletes the row again. A task --after others that
// are not done yet is queued (phase waiting) and started by the flow.
func (a *App) Launch(ctx context.Context, la LaunchArgs) error {
	if strings.TrimSpace(la.Title) == "" {
		return fmt.Errorf("--title is required")
	}
	if la.Isolation != "" && la.Isolation != "none" && la.Isolation != "worktree" {
		return fmt.Errorf("--isolation must be none or worktree")
	}
	if la.Engine == "" {
		la.Engine = "claude"
	}
	eng, err := a.Engine(la.Engine)
	if err != nil {
		return err
	}
	cwd := a.Cwd
	if la.Cwd != "" {
		cwd = la.Cwd
	}
	cwd = canonical(cwd)
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not a directory", cwd)
	}
	inPlace := la.Isolation == "none"
	var after []int
	if la.After != "" {
		if after, err = a.parseAfter(ctx, la.After); err != nil {
			return err
		}
	}
	if la.Check != "" {
		spec, err := check.ParseSpec(la.Check)
		if err != nil {
			return err
		}
		if spec.Kind == check.Codex {
			if _, err := a.Engine(check.Codex); err != nil {
				return fmt.Errorf("--check codex needs the codex engine: %v", err)
			}
		}
		la.Check = spec.String()
	}
	if la.Max < 0 || la.Max > 0 && la.Check == "" {
		return fmt.Errorf("--max sets the rounds of --check; it needs --check and a positive number")
	}
	fork := ""
	if la.Fork {
		if la.After != "" {
			return fmt.Errorf("--fork starts the task now, from the conversation as it is; it cannot wait --after other tasks")
		}
		if fork, err = a.forkSource(la.Engine); err != nil {
			return err
		}
	}
	if la.Merge {
		if inPlace {
			return fmt.Errorf("--merge needs a worktree task; an in-place task (--isolation none) leaves its changes uncommitted")
		}
		if la.MergeInto, err = a.mergeTarget(cwd, la.MergeInto); err != nil {
			return err
		}
	}
	args, err := eng.CheckArgs(la.EngineArgs, a.Cwd)
	if err != nil {
		return err
	}
	var raw []byte
	if la.PromptFile != "" {
		if raw, err = os.ReadFile(la.PromptFile); err != nil {
			return fmt.Errorf("cannot read the prompt file %s: %v", la.PromptFile, err)
		}
	} else if a.Stdin != nil {
		raw, _ = io.ReadAll(a.Stdin)
	}
	prompt := strings.TrimSpace(string(raw))
	if prompt == "" {
		if la.PromptFile != "" {
			return fmt.Errorf("the prompt file %s is empty", la.PromptFile)
		}
		return fmt.Errorf("the prompt (stdin) is empty")
	}
	rules := ""
	if !la.NoWorkerRules {
		rules = assets.WorkerRulesFor(la.Engine, inPlace)
		if la.Check != "" || la.Merge {
			rules = strings.TrimRight(rules, "\n") + "\n" + assets.FlowRules()
		}
	}
	db, err := a.Store()
	if err != nil {
		return err
	}
	deps := map[int]store.Task{}
	ready := true
	for _, id := range after {
		d, _ := db.Get(ctx, id)
		deps[id] = d
		ready = ready && d.Phase == store.Done
	}
	phase := store.Running
	if !ready {
		phase = store.Waiting
	}
	t, err := db.Insert(ctx, store.Task{
		Title: la.Title, Cwd: cwd, Engine: la.Engine, Model: la.Model, EngineArgs: args,
		ParentSession: a.SessionKey(), InPlace: inPlace, Prompt: prompt, WorkerRules: rules, Phase: phase,
		After: after, CheckSpec: la.Check, MaxRounds: la.Max, MergeInto: la.MergeInto,
	})
	if err != nil {
		return err
	}
	a.prune(ctx, nil)
	if phase == store.Waiting {
		// It starts later, from a detached csquad without this session's
		// environment: the engine takes what it needs from it now.
		if q, ok := eng.(interface {
			Queue(id int, args []string, inPlace bool, cwd string) ([]string, []string, error)
		}); ok {
			qargs, notes, err := q.Queue(t.ID, args, inPlace, cwd)
			if err == nil {
				err = db.SetEngineArgs(ctx, t.ID, qargs)
			}
			if err != nil {
				_ = db.Delete(context.Background(), t.ID)
				return err
			}
			for _, n := range notes {
				a.warnf("%s\n", n)
			}
		}
		a.printf("queued %s · after %s · %s%s\n", t.Name(), labels(after), cwd, flowPlan(t))
		if la.PromptFile != "" && la.RmPrompt {
			os.Remove(la.PromptFile)
		}
		return nil
	}
	if fork != "" {
		a.saveForkBase(ctx, eng, t.ID, fork)
	}
	l, _ := flock.Try(a.TaskLock(t.ID))
	sid, err := a.startSession(ctx, t, flow.StartPrompt(t, deps), fork)
	if err != nil {
		l.Release()
		os.Remove(a.usageBase(t.ID)) // the number is free again
		_ = db.Delete(context.Background(), t.ID)
		return err
	}
	err = db.SetSession(ctx, t.ID, sid)
	l.Release()
	if err != nil {
		return err
	}
	forked := ""
	if fork != "" {
		forked = "\n  a fork of this session: it starts with the conversation so far"
	}
	a.printf("launched %s · session %s · %s%s%s\n", t.Name(), sid, cwd, forked, flowPlan(t))
	// The prompt is the task's own copy now; a file made just for this launch goes.
	if la.PromptFile != "" && la.RmPrompt {
		os.Remove(la.PromptFile)
	}
	return nil
}

func labels(ids []int) string {
	var s []string
	for _, id := range ids {
		s = append(s, "T"+strconv.Itoa(id))
	}
	return strings.Join(s, ",")
}

// flowPlan is what the flow will do with a task, for launch's output.
func flowPlan(t store.Task) string {
	var parts []string
	if t.CheckSpec != "" {
		parts = append(parts, fmt.Sprintf("check %s, at most %d rounds", t.CheckSpec, t.MaxRounds))
	}
	if t.MergeInto != "" {
		parts = append(parts, "merge into "+t.MergeInto)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n  then: " + strings.Join(parts, " · ")
}

// mergeTarget is --merge's branch: the one given, else the branch of the
// main checkout of cwd's repository. It must exist.
func (a *App) mergeTarget(cwd, branch string) (string, error) {
	g := gitx.Git{Env: a.Env}
	top := g.TopLevel(cwd)
	if top == "" {
		return "", fmt.Errorf("--merge needs a git repository; %s is not in one", cwd)
	}
	wts, err := g.Worktrees(top)
	if err != nil || len(wts) == 0 {
		return "", fmt.Errorf("--merge: cannot list the worktrees of %s: %v", top, err)
	}
	if branch == "" {
		branch = wts[0].Branch
		if branch == "" {
			return "", fmt.Errorf("--merge: the main checkout %s is on a detached HEAD; name the branch: --merge BRANCH", wts[0].Path)
		}
	}
	if _, err := g.Run(top, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err != nil {
		return "", fmt.Errorf("--merge: there is no branch %s in %s", branch, wts[0].Path)
	}
	return branch, nil
}
