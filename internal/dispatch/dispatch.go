// Package dispatch launches queued tasks as Claude Code background sessions
// while keeping the number of active sessions within the configured slots.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ShunL12324/c-squad/internal/claude"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/prompts"
	"github.com/ShunL12324/c-squad/internal/queue"
)

// launchGrace keeps a just-launched task counted as active before its session
// shows up in `claude agents`, and covers a dispatcher that died mid-launch.
const launchGrace = time.Minute

// lockName is the lock file that keeps one dispatcher per data directory.
const lockName = "dispatcher"

// giveUpAfter ends a dispatcher whose steps keep failing, e.g. because claude
// is missing or signed out. Queued tasks stay queued; the next add retries.
const giveUpAfter = 2 * time.Minute

// ErrRunning reports that another dispatcher holds the lock.
var ErrRunning = errors.New("dispatcher already running")

// Dispatcher moves tasks from the queue into background sessions.
type Dispatcher struct {
	Store  *queue.Store
	Config config.Config
	// Sessions and Launch default to the claude package; tests replace them.
	Sessions func(context.Context) (map[string]claude.Session, error)
	Launch   func(context.Context, claude.LaunchOptions) (string, error)
	Trust    func(dir string) error
	Log      io.Writer
	now      func() time.Time
}

func (d *Dispatcher) defaults() {
	if d.Sessions == nil {
		d.Sessions = claude.Sessions
	}
	if d.Launch == nil {
		d.Launch = claude.Launch
	}
	if d.Trust == nil {
		d.Trust = claude.EnsureTrusted
	}
	if d.Log == nil {
		d.Log = io.Discard
	}
	if d.now == nil {
		d.now = time.Now
	}
}

// Occupied counts launched tasks whose sessions hold a slot.
func Occupied(launched []queue.Task, sessions map[string]claude.Session, now time.Time) int {
	n := 0
	for _, t := range launched {
		if s, ok := sessions[t.Session]; ok {
			if s.Active() {
				n++
			}
		} else if now.Sub(t.LaunchedAt) < launchGrace {
			n++
		}
	}
	return n
}

// Step launches as many queued tasks as free slots allow and returns how many
// tasks are still queued afterwards.
func (d *Dispatcher) Step(ctx context.Context) (int, error) {
	d.defaults()
	queued, err := d.Store.List(queue.Queued)
	if err != nil || len(queued) == 0 {
		return len(queued), err
	}
	sessions, err := d.Sessions(ctx)
	if err != nil {
		return len(queued), err
	}
	launched, err := d.Store.List(queue.Launched)
	if err != nil {
		return len(queued), err
	}
	free := d.Config.Slots - Occupied(launched, sessions, d.now())
	remaining := len(queued)
	for _, t := range queued {
		if free <= 0 {
			break
		}
		ok, err := d.Store.Claim(t.ID)
		if err != nil {
			return remaining, err
		}
		remaining--
		if !ok {
			continue // cancelled meanwhile
		}
		free--
		d.launch(ctx, t)
	}
	return remaining, nil
}

func (d *Dispatcher) launch(ctx context.Context, t queue.Task) {
	fail := func(err error) {
		_, _ = fmt.Fprintf(d.Log, "%s %s launch failed: %v\n", stamp(d.now()), t.Label(), err)
		if e := d.Store.Fail(t.ID, err.Error()); e != nil {
			_, _ = fmt.Fprintf(d.Log, "%s %s record failure: %v\n", stamp(d.now()), t.Label(), e)
		}
	}
	if err := d.Trust(t.Cwd); err != nil {
		fail(err)
		return
	}
	model := t.Model
	if model == "" {
		model = d.Config.Model
	}
	id, err := d.Launch(ctx, claude.LaunchOptions{
		Dir: t.Cwd, Name: t.DisplayName(), Prompt: t.Prompt, SystemPrompt: prompts.Worker,
		Agent: t.Agent, Model: model, PermissionMode: d.Config.PermissionMode,
	})
	if err != nil {
		fail(err)
		return
	}
	if err := d.Store.SetSession(t.ID, id); err != nil {
		_, _ = fmt.Fprintf(d.Log, "%s %s record session %s: %v\n", stamp(d.now()), t.Label(), id, err)
		return
	}
	_, _ = fmt.Fprintf(d.Log, "%s %s launched session %s in %s\n", stamp(d.now()), t.Label(), id, t.Cwd)
}

// Run holds the dispatcher lock in dataDir and steps every interval until the
// queue is empty or ctx ends. Transient errors are logged and retried.
//
// Exiting must not strand a task that `add` queued while the dispatcher still
// held the lock: that `add` saw a running dispatcher and started none. So the
// dispatcher releases the lock first and then checks the queue again. An `add`
// that committed before the check is seen here; one that commits after it
// finds the lock free and starts a new dispatcher.
func (d *Dispatcher) Run(ctx context.Context, dataDir string, interval time.Duration) error {
	d.defaults()
	release, err := filelock.Acquire(dataDir, lockName, true)
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrRunning
		}
		return err
	}
	defer func() { release() }()
	_, _ = fmt.Fprintf(d.Log, "%s dispatcher started (pid %d, %d slots)\n", stamp(d.now()), os.Getpid(), d.Config.Slots)
	var failingSince time.Time
	for {
		remaining, err := d.Step(ctx)
		if err != nil {
			_, _ = fmt.Fprintf(d.Log, "%s %v\n", stamp(d.now()), err)
			if failingSince.IsZero() {
				failingSince = d.now()
			} else if d.now().Sub(failingSince) >= giveUpAfter {
				_, _ = fmt.Fprintf(d.Log, "%s giving up after %s of errors; queued tasks stay queued\n", stamp(d.now()), giveUpAfter)
				return err
			}
		} else {
			failingSince = time.Time{}
		}
		if err == nil && remaining == 0 {
			release()
			queued, err := d.Store.List(queue.Queued)
			if err == nil && len(queued) == 0 {
				_, _ = fmt.Fprintf(d.Log, "%s queue empty; dispatcher exiting\n", stamp(d.now()))
				return nil
			}
			next, err := filelock.Acquire(dataDir, lockName, true)
			if err != nil {
				// Another dispatcher took over the new work.
				release = func() {}
				return nil
			}
			release = next
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Running reports whether a dispatcher holds the lock in dataDir.
func Running(dataDir string) bool {
	release, err := filelock.Acquire(dataDir, lockName, true)
	if err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	release()
	return false
}

// Start launches `exe dispatch` detached from the caller's session when no
// dispatcher is running. Output is appended to dispatcher.log in dataDir. Two
// callers racing here are harmless: the loser's dispatcher exits on the lock.
func Start(exe, dataDir string) error {
	if Running(dataDir) {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(LogPath(dataDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.Command(exe, "dispatch")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start dispatcher: %w", err)
	}
	return cmd.Process.Release()
}

// LogPath is the dispatcher log inside dataDir.
func LogPath(dataDir string) string { return filepath.Join(dataDir, "dispatcher.log") }

func stamp(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
