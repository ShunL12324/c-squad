package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/install"
	"github.com/ShunL12324/c-squad/internal/statusline"
	"github.com/ShunL12324/c-squad/internal/store"
)

// Statusline is the status line command: the user's own line, then the
// ledger of this session's tasks. It never fails.
func (a *App) Statusline() {
	var input []byte
	if a.Stdin != nil {
		input, _ = io.ReadAll(a.Stdin)
	}
	saved, _ := install.LoadSaved(a.Home)
	a.NoStart = true // starting the Codex daemon does not fit the status line's budget
	columns, err := strconv.Atoi(a.Env.Get("COLUMNS"))
	if err != nil || columns <= 0 {
		columns = 120
	}
	out := statusline.Run(statusline.Options{
		Stdin:    input,
		Command:  saved.CommandLine(),
		Env:      a.Env,
		CacheDir: a.cacheDir(),
		Columns:  columns,
		NoColor:  a.Env.Get("NO_COLOR") != "",
		Now:      a.Now,
		Query:    a.statusQuery,
	})
	io.WriteString(a.Stdout, out)
}

func (a *App) cacheDir() string { return a.Home + string(os.PathSeparator) + "cache" }

// usageState is where the engine keeps its reading position in a task's
// session (the status line's samples are cache/T12.usage.json).
func (a *App) usageState(id int) string {
	return filepath.Join(a.cacheDir(), fmt.Sprintf("T%d.read.json", id))
}

// statusQuery returns this session's unfinished tasks. Most sessions never
// dispatch a task: then no engine is asked.
func (a *App) statusQuery(ctx context.Context, in statusline.Input) ([]statusline.Observed, error) {
	owners := a.owners(in.Key())
	if len(owners) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(a.DBFile()); err != nil {
		return nil, nil
	}
	db, err := a.Store()
	if err != nil {
		return nil, err
	}
	tasks, err := db.List(ctx, store.Filter{Parents: owners})
	if err != nil || len(tasks) == 0 {
		return nil, err
	}
	live, err := a.observe(ctx, tasks)
	if err != nil && len(live) == 0 {
		for _, na := range notAnswering(err) {
			return nil, na // the ledger says so, by its Short
		}
		return nil, err
	}
	tasks = a.clearGone(ctx, tasks, live)
	a.prune(ctx, gone(tasks, live))
	a.maybeAdvance(ctx, tasks, live)
	marks := len(engine.Names()) > 1
	var out []statusline.Observed
	for _, t := range tasks {
		l, ok := live[t.ID]
		v := view(t, l, ok)
		key := v.Key
		if key == "starting" {
			key = "stopped"
		}
		text := v.Text
		if key == "needs_you" {
			text = "" // the ledger adds the reason from the note
		}
		o := statusline.Observed{
			Task:      statusline.Task{ID: t.ID, Title: t.Title, State: key, Text: text, Note: t.Note},
			SessionID: t.SessionID,
			StartedAt: l.StartedAt,
		}
		if o.StartedAt.IsZero() {
			o.StartedAt = time.UnixMilli(t.CreatedAt)
		}
		if marks {
			if e, err := a.Engine(t.Engine); err == nil {
				o.Mark = e.Mark()
			}
		}
		if e, err := a.Engine(t.Engine); err == nil && t.SessionID != "" {
			sid := t.SessionID
			if l.SessionID != "" {
				sid = l.SessionID
			}
			if act, ok := e.(interface {
				Activity(string) (time.Time, bool)
			}); ok {
				o.Activity = func() (time.Time, bool) { return act.Activity(sid) }
			}
			if ur, ok := e.(engine.UsageReader); ok {
				id := t.ID
				o.ReadUsage = func(ctx context.Context) (statusline.Reading, error) {
					u, err := a.taskUsage(ctx, id, ur, sid)
					return statusline.Reading{Known: u.Known, Fresh: u.Fresh(), Activity: u.Activity}, err
				}
			}
		}
		out = append(out, o)
	}
	return out, nil
}
