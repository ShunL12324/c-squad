// Package app implements csquad's commands on top of the store, the engines
// and git.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	_ "github.com/ShunL12324/c-squad/internal/engine/claude" // registers the engine
	"github.com/ShunL12324/c-squad/internal/proc"
	"github.com/ShunL12324/c-squad/internal/store"
)

// Version is csquad's version; the build may set it with -ldflags.
var Version = "3.0.0-dev"

// App is one csquad invocation.
type App struct {
	Home      string // $CSQUAD_HOME
	ClaudeDir string // $CLAUDE_CONFIG_DIR
	Env       proc.Env
	Cwd       string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Now       func() time.Time

	db      *store.Store
	engines map[string]engine.Engine
}

// New builds an App from the process environment.
func New(env proc.Env, stdin io.Reader, stdout, stderr io.Writer) *App {
	home, _ := os.UserHomeDir()
	if h := env.Get("HOME"); h != "" && !proc.Windows {
		home = h
	}
	a := &App{Env: env, Stdin: stdin, Stdout: stdout, Stderr: stderr, Now: time.Now}
	a.Home = env.Get("CSQUAD_HOME")
	if a.Home == "" {
		a.Home = filepath.Join(home, ".csquad")
	}
	a.ClaudeDir = env.Get("CLAUDE_CONFIG_DIR")
	if a.ClaudeDir == "" {
		a.ClaudeDir = filepath.Join(home, ".claude")
	}
	a.Home, _ = filepath.Abs(a.Home)
	a.ClaudeDir, _ = filepath.Abs(a.ClaudeDir)
	a.Cwd, _ = os.Getwd()
	return a
}

// Close releases the database.
func (a *App) Close() {
	if a.db != nil {
		a.db.Close()
	}
}

// DBFile is the database's path.
func (a *App) DBFile() string { return filepath.Join(a.Home, "csquad.db") }

// Store opens the database on first use.
func (a *App) Store() (*store.Store, error) {
	if a.db == nil {
		db, err := store.Open(a.DBFile())
		if err != nil {
			return nil, err
		}
		db.Now = a.Now
		a.db = db
	}
	return a.db, nil
}

// Engine returns an engine by name.
func (a *App) Engine(name string) (engine.Engine, error) {
	if e, ok := a.engines[name]; ok {
		return e, nil
	}
	e, err := engine.New(name, engine.Config{Home: a.Home, ClaudeDir: a.ClaudeDir, Env: a.Env})
	if err != nil {
		return nil, err
	}
	if a.engines == nil {
		a.engines = map[string]engine.Engine{}
	}
	a.engines[name] = e
	return e, nil
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Stdout, format, args...) }
func (a *App) warnf(format string, args ...any)  { fmt.Fprintf(a.Stderr, format, args...) }

// SessionKey is the dispatching session: "claude:<id>" from
// CLAUDE_CODE_SESSION_ID, "" outside a session.
func (a *App) SessionKey() string {
	if id := a.Env.Get("CLAUDE_CODE_SESSION_ID"); id != "" {
		return "claude:" + id
	}
	return ""
}

// Owners are the parent_session values that count as this session; nil
// (outside a session) means every task.
func (a *App) Owners() []string {
	return a.owners(a.Env.Get("CLAUDE_CODE_SESSION_ID"))
}

func (a *App) owners(sid string) []string {
	if sid == "" {
		return nil
	}
	return []string{"claude:" + sid}
}

// owns reports whether this session dispatched t (always true outside a
// session).
func owns(owners []string, t store.Task) bool {
	if owners == nil {
		return true
	}
	for _, o := range owners {
		if t.ParentSession == o {
			return true
		}
	}
	return false
}

var label = regexp.MustCompile(`^[Tt]?(\d+)$`)

// ParseLabel reads "T12" or "12".
func ParseLabel(s string) (int, error) {
	m := label.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("expected a task like T12, got %q", s)
	}
	return strconv.Atoi(m[1])
}

// task finds a task by label, refusing another session's unless all.
func (a *App) task(ctx context.Context, s string, all bool, verb string) (store.Task, error) {
	id, err := ParseLabel(s)
	if err != nil {
		return store.Task{}, err
	}
	db, err := a.Store()
	if err != nil {
		return store.Task{}, err
	}
	t, err := db.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return t, fmt.Errorf("T%d is not a csquad task (csquad status --all lists them)", id)
	}
	if err != nil {
		return t, err
	}
	if !all && !owns(a.Owners(), t) {
		return t, fmt.Errorf("%s was launched by another session; pass --all to %s it anyway", t.Label(), verb)
	}
	return t, nil
}

// observe returns the live state of each task with a session, one engine
// query per engine. Tasks of an engine that failed are missing from the map
// and the error names the engine.
func (a *App) observe(ctx context.Context, tasks []store.Task) (map[int]engine.Live, error) {
	byEngine := map[string][]store.Task{}
	for _, t := range tasks {
		if t.SessionID != "" {
			byEngine[t.Engine] = append(byEngine[t.Engine], t)
		}
	}
	out := map[int]engine.Live{}
	var errs []string
	names := make([]string, 0, len(byEngine))
	for n := range byEngine {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		e, err := a.Engine(name)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		ids := make([]string, 0, len(byEngine[name]))
		for _, t := range byEngine[name] {
			ids = append(ids, t.SessionID)
		}
		states, err := e.States(ctx, ids)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, t := range byEngine[name] {
			out[t.ID] = states[t.SessionID]
		}
	}
	if len(errs) > 0 {
		return out, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// View is how a task shows: a state key (statusline keys plus "gone") and
// its text.
type View struct {
	Key  string
	Text string
}

// view combines a task's phase and its observed session state.
func view(t store.Task, l engine.Live, observed bool) View {
	switch t.Phase {
	case store.Waiting:
		var ids []string
		for _, id := range t.After {
			ids = append(ids, "T"+strconv.Itoa(id))
		}
		return View{"waiting", strings.TrimSpace("waiting " + strings.Join(ids, ","))}
	case store.Checking, store.Fixing:
		return View{t.Phase, fmt.Sprintf("%s %d/%d", t.Phase, max(1, t.Round), t.MaxRounds)}
	case store.Merging, store.Ready, store.Done, store.Failed, store.Cancelled:
		return View{t.Phase, t.Phase}
	case store.NeedsYou:
		return View{"needs_you", "needs you"}
	}
	if t.SessionID == "" {
		return View{"starting", "starting"}
	}
	if !observed {
		return View{"unknown", "unknown"}
	}
	return View{l.State, stateText(l.State)}
}

func stateText(s string) string {
	if s == engine.NeedsInput {
		return "needs input"
	}
	return strings.ReplaceAll(s, "_", " ")
}

// prune removes the settings and prompt files of tasks that are finished or
// not in the table (a failed launch), and of tasks observed gone: the
// settings can hold a provider token. Files are listed before the table is
// read, so a launch in flight (its row is written first) is never judged.
func (a *App) prune(ctx context.Context, gone []int) {
	type file struct {
		path string
		id   int
	}
	var files []file
	re := regexp.MustCompile(`^T(\d+)\.(json|md)$`)
	for _, d := range []string{"settings", "prompts"} {
		entries, _ := os.ReadDir(filepath.Join(a.Home, d))
		for _, e := range entries {
			if m := re.FindStringSubmatch(e.Name()); m != nil {
				id, _ := strconv.Atoi(m[1])
				files = append(files, file{filepath.Join(a.Home, d, e.Name()), id})
			}
		}
	}
	if len(files) == 0 {
		return
	}
	db, err := a.Store()
	if err != nil {
		return
	}
	all, err := db.List(ctx, store.Filter{Finished: true})
	if err != nil {
		return
	}
	keep := map[int]bool{}
	for _, t := range all {
		keep[t.ID] = !store.Finished(t.Phase)
	}
	for _, id := range gone {
		keep[id] = false
	}
	for _, f := range files {
		if !keep[f.id] {
			os.Remove(f.path)
		}
	}
}

// gone lists the tasks whose session the engine no longer lists.
func gone(tasks []store.Task, live map[int]engine.Live) []int {
	var out []int
	for _, t := range tasks {
		if l, ok := live[t.ID]; ok && l.State == engine.Gone {
			out = append(out, t.ID)
		}
	}
	return out
}

// ago is a duration for humans: 42s, 5m, 3h, 2d.
func ago(d time.Duration) string {
	s := math.Max(0, math.Round(d.Seconds()))
	switch {
	case s < 60:
		return fmt.Sprintf("%.0fs", s)
	case s < 3600:
		return fmt.Sprintf("%.0fm", math.Round(s/60))
	case s < 86400:
		return fmt.Sprintf("%.0fh", math.Round(s/3600))
	}
	return fmt.Sprintf("%.0fd", math.Round(s/86400))
}

func (a *App) shortDir(dir string) string {
	home := a.Env.Get("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + dir[len(home):]
	}
	return dir
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func canonical(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}
