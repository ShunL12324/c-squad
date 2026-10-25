package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// `claude agents` asks Claude Code's background service over its control
// socket. A service that stops answering (seen with Claude Code 2.1.295
// after 16 hours up) leaves the client waiting forever, deaf to SIGTERM. So
// every call has a deadline, after which its whole process group is killed
// (its tree on Windows); every call is recorded in cache/claude-agents/
// while it runs, and one that outlives orphanAge (its csquad died) is
// killed by the next csquad that looks.
const (
	// AgentsTimeout is how long a `claude agents` call of a command may
	// take; it answers in well under a second (about 0.2 s).
	AgentsTimeout = 8 * time.Second
	orphanAge     = time.Minute
	// downFor: after a call got no answer, this process's next calls fail
	// at once for this long instead of waiting again (finish's own
	// observation, then the queue's).
	downFor = 30 * time.Second
	// shareWait is how long the status line waits for a call in flight.
	shareWait = 1500 * time.Millisecond
)

// Restart is how the user restarts Claude Code's background service
// without ending its sessions (Claude Code 2.1.295 to 2.1.310). The service
// starts on demand, so `claude daemon stop` alone refuses it (--any); with
// --keep-workers the sessions keep running, and a new service started
// within about a minute adopts them. `claude agents` (agent view) or `claude
// --bg` starts one, `claude agents --json` does not. A session left without
// a service for longer ends as failed.
const Restart = "claude daemon stop --any --keep-workers, then run claude agents (agent view) right away; running sessions keep running and the new service adopts them"

type down struct {
	mu  sync.Mutex
	err *engine.NotAnswering
	at  time.Time
}

// lastList is the last list `claude agents` printed, kept for the status
// line (another csquad's answer) and for what status shows when the
// service does not answer.
type lastList struct {
	At   int64     `json:"at"`
	List []Session `json:"list"`
}

func (e *Engine) cacheDir() string {
	if e.cfg.Home == "" {
		return ""
	}
	return filepath.Join(e.cfg.Home, "cache")
}

// callsDir records the `claude agents` calls running.
func (e *Engine) callsDir() string {
	if d := e.cacheDir(); d != "" {
		return filepath.Join(d, "claude-agents")
	}
	return ""
}

func (e *Engine) lastFile() string {
	if d := e.cacheDir(); d != "" {
		return filepath.Join(d, "claude-agents.json")
	}
	return ""
}

func (e *Engine) loadLast() (lastList, bool) {
	var l lastList
	f := e.lastFile()
	if f == "" {
		return l, false
	}
	b, err := os.ReadFile(f)
	if err != nil || json.Unmarshal(b, &l) != nil || l.At == 0 {
		return l, false
	}
	return l, true
}

// timeout is AgentsTimeout, or CSQUAD_OBSERVE_TIMEOUT_MS (tests).
func (e *Engine) timeout() time.Duration {
	if ms, err := strconv.Atoi(e.cfg.Env.Get("CSQUAD_OBSERVE_TIMEOUT_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return AgentsTimeout
}

// notAnswering is the error of a call the service did not answer.
func notAnswering(detail string) *engine.NotAnswering {
	return &engine.NotAnswering{
		Service: "Claude Code's background service",
		Detail:  detail,
		Short:   "claude agents not answering",
		Fix:     "restart it: " + Restart,
	}
}

// Agents lists the sessions Claude Code knows, completed ones included.
func (e *Engine) Agents(ctx context.Context) ([]Session, error) {
	e.down.mu.Lock()
	if d := e.down.err; d != nil && time.Since(e.down.at) < downFor {
		e.down.mu.Unlock()
		return nil, d
	}
	e.down.mu.Unlock()
	start := time.Now()
	running := proc.Sweep(e.callsDir(), orphanAge, start)
	if e.cfg.StatusLine && len(running) > 0 {
		list, ok, err := e.share(ctx, start)
		if ok || err != nil {
			return list, err
		}
	}
	list, err := e.agents(ctx, start)
	var na *engine.NotAnswering
	e.down.mu.Lock()
	if errors.As(err, &na) {
		e.down.err, e.down.at = na, time.Now()
	} else if err == nil {
		e.down.err = nil
	}
	e.down.mu.Unlock()
	return list, err
}

// share waits a little for the call another csquad is making and takes its
// answer: the status line never starts a second call while one runs. ok is
// false when that call ended without an answer: then the caller asks
// itself.
func (e *Engine) share(ctx context.Context, start time.Time) ([]Session, bool, error) {
	wait := shareWait
	if dl, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(dl)/2)
	}
	for until := start.Add(wait); ; {
		if l, ok := e.loadLast(); ok && l.At >= start.UnixMilli() {
			return l.List, true, nil
		}
		running := proc.Sweep(e.callsDir(), orphanAge, time.Now())
		if len(running) == 0 {
			return nil, false, nil
		}
		if !time.Now().Before(until) {
			oldest := running[0].At
			for _, r := range running {
				oldest = min(oldest, r.At)
			}
			age := time.Since(time.UnixMilli(oldest)).Round(time.Second)
			return nil, false, notAnswering(fmt.Sprintf("an earlier claude agents call has had no answer for %s", age))
		}
		select {
		case <-ctx.Done():
			return nil, false, notAnswering("an earlier claude agents call is still running")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// agents runs `claude agents --json --all` within the timeout (or ctx's
// deadline, when sooner).
func (e *Engine) agents(ctx context.Context, start time.Time) ([]Session, error) {
	c, err := proc.Resolve("claude", []string{"agents", "--json", "--all"}, e.cfg.Env)
	if err != nil {
		return nil, err
	}
	limit := e.timeout()
	tctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := c.Command(tctx)
	cmd.Env = e.cfg.Env
	proc.Bounded(cmd, time.Second)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot run claude: %w", err)
	}
	by := "command"
	if e.cfg.StatusLine {
		by = "statusline"
	}
	forget := proc.Track(e.callsDir(), cmd, by, start)
	err = cmd.Wait()
	forget()
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		took := time.Since(start).Round(100 * time.Millisecond)
		return nil, notAnswering(fmt.Sprintf("claude agents got no answer in %s", took))
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(ansi.ReplaceAllString(stderr.String(), ""))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("claude agents failed: %s", firstLine(msg))
	}
	var list []Session
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		return nil, fmt.Errorf("claude agents printed no JSON list: %v", err)
	}
	if f := e.lastFile(); f != "" {
		engine.SaveState(f, lastList{At: time.Now().UnixMilli(), List: list})
	}
	return list, nil
}

// withLast adds what the last list showed of ids to a NotAnswering error.
func (e *Engine) withLast(err error, ids []string) error {
	var na *engine.NotAnswering
	if !errors.As(err, &na) {
		return err
	}
	out := *na
	if l, ok := e.loadLast(); ok {
		out.Last, out.LastAt = states(l.List, ids), time.UnixMilli(l.At)
	}
	return &out
}
