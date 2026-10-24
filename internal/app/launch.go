package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/engine"
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
}

// Launch starts a task: the next number, a row in the table, then the
// session. A failed start deletes the row again.
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
	inPlace := la.Isolation == "none"
	rules := ""
	if !la.NoWorkerRules {
		rules = assets.WorkerRulesFor(la.Engine, inPlace)
	}
	db, err := a.Store()
	if err != nil {
		return err
	}
	t, err := db.Insert(ctx, store.Task{
		Title: la.Title, Cwd: cwd, Engine: la.Engine, Model: la.Model, EngineArgs: args,
		ParentSession: a.SessionKey(), InPlace: inPlace, Prompt: prompt, WorkerRules: rules, Phase: store.Running,
	})
	if err != nil {
		return err
	}
	a.prune(ctx, nil)
	started, err := eng.Start(ctx, engine.StartSpec{
		ID: t.ID, Title: t.Title, Cwd: cwd, Model: la.Model, Prompt: prompt, Rules: rules, InPlace: inPlace, Args: args,
	})
	if err != nil {
		_ = db.Delete(context.Background(), t.ID)
		return err
	}
	if err := db.SetSession(ctx, t.ID, started.SessionID); err != nil {
		return err
	}
	for _, n := range started.Notes {
		a.warnf("%s\n", n)
	}
	a.printf("launched %s · session %s · %s\n", t.Name(), started.SessionID, cwd)
	// The prompt is the task's own copy now; a file made just for this launch goes.
	if la.PromptFile != "" && la.RmPrompt {
		os.Remove(la.PromptFile)
	}
	return nil
}
