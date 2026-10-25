// Package claude runs tasks as Claude Code background sessions: claude --bg
// to start, claude agents --json --all to observe, the session transcript to
// peek, the session inbox socket to message, claude rm to remove.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// Name is the engine's name in the tasks table.
const Name = "claude"

func init() {
	engine.Register(Name, func(c engine.Config) engine.Engine { return New(c) })
}

// Engine is the Claude Code engine.
type Engine struct {
	cfg engine.Config
	// PermissionMode is the default --permission-mode of a task.
	PermissionMode string
	down           down // a call that got no answer
}

// New returns a Claude Code engine.
func New(c engine.Config) *Engine {
	return &Engine{cfg: c, PermissionMode: "bypassPermissions"}
}

func (e *Engine) Name() string { return Name }
func (e *Engine) Mark() string { return "✻" }

// Session is one entry of `claude agents --json --all`.
type Session struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionId"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	State      string          `json:"state"`
	Status     string          `json:"status"`
	Tempo      string          `json:"tempo"`
	WaitingFor json.RawMessage `json:"waitingFor"`
	Cwd        string          `json:"cwd"`
	StartedAt  int64           `json:"startedAt"`
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// run runs claude with args and returns its combined output.
func (e *Engine) run(ctx context.Context, dir string, args ...string) (string, error) {
	c, err := proc.Resolve("claude", args, e.cfg.Env)
	if err != nil {
		return "", err
	}
	cmd := c.Command(ctx)
	cmd.Dir = dir
	cmd.Env = e.cfg.Env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	text := strings.TrimSpace(ansi.ReplaceAllString(out.String(), ""))
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if text == "" {
				text = err.Error()
			}
			return text, errors.New(text)
		}
		return text, fmt.Errorf("cannot run claude: %w", err)
	}
	return text, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// RawState mirrors how agent view labels a session. A session waiting on the
// user (status waiting, or waitingFor while not busy) needs input whatever
// its state: a message held for approval after the turn ended reads `state:
// done, status: waiting, waitingFor: "permission prompt"` (Claude Code
// 2.1.295). Then a finished state wins (crashed counts as failed);
// otherwise the status decides: busy or shell is working, tempo blocked or
// state blocked needs input, idle is idle. `state` alone stays "working"
// after a turn ends.
func RawState(s Session) string {
	busy := s.Status == "busy" || s.Status == "shell"
	waitingFor := len(s.WaitingFor) > 0 && string(s.WaitingFor) != "null" && string(s.WaitingFor) != `""`
	if s.Status == "waiting" || waitingFor && !busy {
		return engine.NeedsInput
	}
	switch s.State {
	case "done", "failed", "stopped":
		return s.State
	case "crashed":
		return engine.Failed
	}
	switch {
	case busy:
		return engine.Working
	case s.Tempo == "blocked" || s.State == "blocked":
		return engine.NeedsInput
	case s.Status == "idle":
		return engine.Idle
	case s.State != "":
		return s.State
	case s.Status != "":
		return s.Status
	}
	return "unknown"
}

// match finds the session for a stored id (the short id `claude --bg`
// prints, or a full session id).
func match(list []Session, id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	for _, s := range list {
		if s.ID == id || s.SessionID == id {
			return s, true
		}
	}
	for _, s := range list {
		if strings.HasPrefix(s.SessionID, id) || s.ID != "" && strings.HasPrefix(id, s.ID+"-") {
			return s, true
		}
	}
	return Session{}, false
}

// States observes sessions with one `claude agents` call.
func (e *Engine) States(ctx context.Context, ids []string) (map[string]engine.Live, error) {
	list, err := e.Agents(ctx)
	if err != nil {
		return nil, e.withLast(err, ids)
	}
	return states(list, ids), nil
}

func states(list []Session, ids []string) map[string]engine.Live {
	out := map[string]engine.Live{}
	for _, id := range ids {
		s, ok := match(list, id)
		if !ok {
			out[id] = engine.Live{State: engine.Gone}
			continue
		}
		l := engine.Live{State: RawState(s), SessionID: s.SessionID, Cwd: s.Cwd}
		if l.SessionID == "" {
			l.SessionID = s.ID
		}
		if s.StartedAt > 0 {
			l.StartedAt = time.UnixMilli(s.StartedAt)
		}
		out[id] = l
	}
	return out
}

// Stop interrupts a session; its conversation is kept.
func (e *Engine) Stop(ctx context.Context, id string) error {
	_, err := e.run(ctx, "", "stop", id)
	return err
}

var (
	discardToken = regexp.MustCompile(`--discard-unpushed[ =]+["']?([^\s"'` + "`" + `]+)`)
	forceToken   = regexp.MustCompile(`--force-remove-worktree[ =]+["']?([^\s"'` + "`" + `]+)`)
)

// Remove deletes a session with `claude rm`. When claude refuses and prints a
// value for --discard-unpushed or --force-remove-worktree, it reruns with
// that value only if the session's own worktree (its cwd) is one of merged:
// verified merged and already removed. Otherwise the refusal is returned.
func (e *Engine) Remove(ctx context.Context, id string, merged []string) (string, error) {
	var cwd string
	var na *engine.NotAnswering
	if list, err := e.Agents(ctx); err == nil {
		if s, ok := match(list, id); ok {
			cwd = s.Cwd
		} else {
			return "session " + id + " was already removed", nil
		}
	} else if errors.As(err, &na) {
		return "", err // claude rm would wait for the same service
	}
	out, err := e.run(ctx, "", "rm", id)
	if err == nil {
		return "removed session " + id, nil
	}
	var extra []string
	if m := discardToken.FindStringSubmatch(out); m != nil {
		extra = append(extra, "--discard-unpushed", m[1])
	}
	if m := forceToken.FindStringSubmatch(out); m != nil {
		extra = append(extra, "--force-remove-worktree", m[1])
	}
	if len(extra) == 0 {
		return "", fmt.Errorf("claude rm %s refused: %s", id, out)
	}
	if cwd == "" || !contains(merged, cwd) {
		return "", fmt.Errorf("claude rm %s refused (%s); its worktree %s was not verified merged, so the session is left", id, firstLine(out), orUnknown(cwd))
	}
	if out2, err := e.run(ctx, "", append([]string{"rm", id}, extra...)...); err != nil {
		return "", fmt.Errorf("claude rm %s %s refused: %s", id, strings.Join(extra, " "), out2)
	}
	return "removed session " + id + " (" + strings.Join(extra, " ") + ", its worktree was verified merged)", nil
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func contains(paths []string, p string) bool {
	cp := canonical(p)
	for _, q := range paths {
		if canonical(q) == cp {
			return true
		}
	}
	return false
}

func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	} else if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		p = filepath.Join(r, filepath.Base(p))
	}
	p = filepath.Clean(p)
	if proc.Windows {
		p = strings.ToLower(p)
	}
	return p
}

// Takeover is the command that attaches the user to the session.
func (e *Engine) Takeover(id string) []string { return []string{"claude", "attach", id} }

// claudeDir is $CLAUDE_CONFIG_DIR or ~/.claude.
func (e *Engine) claudeDir() string {
	if e.cfg.ClaudeDir != "" {
		return e.cfg.ClaudeDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
