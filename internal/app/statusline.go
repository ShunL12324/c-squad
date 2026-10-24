package app

import (
	"context"
	"io"
	"os"
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
		return nil, err
	}
	a.prune(ctx, gone(tasks, live))
	marks := len(engine.Names()) > 1
	var out []statusline.Observed
	for _, t := range tasks {
		l, ok := live[t.ID]
		v := view(t, l, ok)
		key := v.Key
		if key == engine.Gone || key == "starting" {
			key = "stopped"
		}
		o := statusline.Observed{
			Task:      statusline.Task{ID: t.ID, Title: t.Title, State: key, Text: v.Text, Note: t.Note},
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
		if e, err := a.Engine(t.Engine); err == nil {
			if act, ok := e.(interface {
				Activity(string) (time.Time, bool)
			}); ok {
				sid := t.SessionID
				if l.SessionID != "" {
					sid = l.SessionID
				}
				o.Activity = func() (time.Time, bool) { return act.Activity(sid) }
			}
		}
		out = append(out, o)
	}
	return out, nil
}
