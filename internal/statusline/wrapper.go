package statusline

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// Input is the part of Claude Code's status line JSON csquad reads.
type Input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// Key is the session the status line runs for: its id, else its
// transcript's name.
func (in Input) Key() string {
	if in.SessionID != "" {
		return in.SessionID
	}
	if in.TranscriptPath != "" {
		return strings.TrimSuffix(filepath.Base(in.TranscriptPath), ".jsonl")
	}
	return ""
}

// Observed is a task as the query sees it; Run adds the time in its state.
type Observed struct {
	Task
	SessionID string
	StartedAt time.Time // when the session started
	Activity  func() (time.Time, bool)
}

// Options are what Run needs; Query returns this session's tasks.
type Options struct {
	Stdin     []byte
	Command   string // the user's own status line command ("" = none)
	Env       proc.Env
	CacheDir  string
	Columns   int
	NoColor   bool
	Now       func() time.Time
	Query     func(ctx context.Context, in Input) ([]Observed, error)
	OwnBudget time.Duration // the user's command; default 3 s
	Budget    time.Duration // the task query; default 3.5 s
}

// Stale is how long a failing part shows its last good output.
const Stale = 2 * time.Minute

// cache is the last good output per session, and when each task entered the
// state it was last seen in (Claude Code reports no such time).
type cache struct {
	Own     string           `json:"own"`
	OwnAt   int64            `json:"ownAt"`
	Tasks   []Task           `json:"tasks"`
	TasksAt int64            `json:"tasksAt"`
	Since   map[string]since `json:"since"`
}

type since struct {
	State string `json:"state"`
	Since int64  `json:"since"`
}

// Run prints the user's status line, then the ledger of this session's
// tasks. Claude Code blanks the whole status line when the command fails,
// times out or prints nothing, so the user's command and the task query run
// in parallel with timeouts, and a part that fails falls back to its last
// good output (up to Stale old) instead of disappearing. Run never fails.
func Run(o Options) string {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.OwnBudget == 0 {
		o.OwnBudget = 3 * time.Second
	}
	if o.Budget == 0 {
		o.Budget = 3500 * time.Millisecond
	}
	var in Input
	_ = json.Unmarshal(o.Stdin, &in)
	key := in.Key()

	var wg sync.WaitGroup
	var own string
	ownOK := true
	if o.Command != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			own, ownOK = runOwn(o.Command, o.Stdin, o.Env, o.OwnBudget)
		}()
	}
	var observed []Observed
	var qerr error
	if o.Query != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), o.Budget)
			defer cancel()
			observed, qerr = o.Query(ctx, in)
		}()
	}
	wg.Wait()

	c := load(o.CacheDir, key)
	now := o.Now()
	own = strings.TrimRight(own, "\r\n")
	switch {
	case !ownOK || own == "" && o.Command != "":
		if c.Own != "" && now.UnixMilli()-c.OwnAt < Stale.Milliseconds() {
			own = c.Own
		}
	case own != "":
		c.Own, c.OwnAt = own, now.UnixMilli()
	}
	var tasks []Task
	if qerr == nil {
		tasks = withSince(observed, &c, now)
		c.Tasks, c.TasksAt = tasks, now.UnixMilli()
	} else if now.UnixMilli()-c.TasksAt < Stale.Milliseconds() {
		tasks = c.Tasks
	}
	save(o.CacheDir, key, c)
	parts := []string{}
	if own != "" {
		parts = append(parts, own)
	}
	parts = append(parts, Layout(tasks, o.Columns, now, o.NoColor)...)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n"
}

// withSince adds when each task entered its state: unchanged since the last
// refresh keeps its time; a change dates from now; a state seen for the first
// time dates from the session's start when working, else from its last
// activity (when it stopped).
func withSince(list []Observed, c *cache, now time.Time) []Task {
	next := map[string]since{}
	out := make([]Task, len(list))
	for i, o := range list {
		t := o.Task
		k := strconv.Itoa(t.ID) + "/" + o.SessionID
		p, seen := c.Since[k]
		switch {
		case seen && p.State == t.State+t.Text:
			t.Since = time.UnixMilli(p.Since)
		case seen:
			t.Since = now
		case t.State == Working && !o.StartedAt.IsZero():
			t.Since = o.StartedAt
		default:
			t.Since = o.StartedAt
			if o.Activity != nil {
				if at, ok := o.Activity(); ok {
					t.Since = at
				}
			}
			if t.Since.IsZero() {
				t.Since = now
			}
		}
		next[k] = since{t.State + t.Text, t.Since.UnixMilli()}
		out[i] = t
	}
	c.Since = next
	return out
}

// runOwn runs the user's status line command as Claude Code does, feeding it
// Claude's JSON; ok is false when it fails, exits non-zero or times out.
func runOwn(command string, stdin []byte, env proc.Env, budget time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	cmd := proc.Shell(command, env).Command(ctx)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.WaitDelay = 200 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func cacheFile(dir, key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(dir, "statusline-"+hex.EncodeToString(sum[:8])+".json")
}

func load(dir, key string) cache {
	var c cache
	if dir == "" || key == "" {
		return c
	}
	if b, err := os.ReadFile(cacheFile(dir, key)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func save(dir, key string, c cache) {
	if dir == "" || key == "" {
		return
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	b, _ := json.Marshal(c)
	f := cacheFile(dir, key)
	tmp := f + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, f) != nil {
		os.Remove(tmp)
	}
}
