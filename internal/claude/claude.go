// Package claude drives the Claude Code CLI: it lists background sessions,
// launches new ones, marks workspaces trusted and reads session transcripts.
// Everything here depends on Claude Code behaviour that is observable but not
// a stable interface, so parsing is tolerant and failures carry the CLI output.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Session states reported by `claude agents --json` for background sessions.
const (
	StateWorking = "working"
	StateBlocked = "blocked"
	StateDone    = "done"
	StateFailed  = "failed"
	StateStopped = "stopped"
)

// Session is one entry of `claude agents --json --all`.
type Session struct {
	// ID is the short background session ID printed by `claude --bg`.
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	// Status is busy, waiting or idle.
	Status string `json:"status"`
	// State is working, blocked, done, failed or stopped.
	State      string `json:"state"`
	WaitingFor string `json:"waitingFor"`
	StartedAt  int64  `json:"startedAt"`
}

// Active reports whether the session occupies a queue slot: it is working, or
// it is blocked waiting for the user.
func (s Session) Active() bool { return s.State == StateWorking || s.State == StateBlocked }

// Binary returns the claude executable: $CSQUAD_CLAUDE when set (tests use a
// fake), otherwise claude from PATH.
func Binary() (string, error) {
	if bin := os.Getenv("CSQUAD_CLAUDE"); bin != "" {
		return bin, nil
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return "", errors.New("claude not found on PATH; install Claude Code and sign in first")
	}
	return bin, nil
}

// Sessions returns the background sessions Claude Code knows about, including
// finished ones, keyed by short ID.
func Sessions(ctx context.Context) (map[string]Session, error) {
	out, err := run(ctx, "", 30*time.Second, "agents", "--json", "--all")
	if err != nil {
		return nil, err
	}
	var list []Session
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse claude agents --json: %w", err)
	}
	sessions := make(map[string]Session, len(list))
	for _, s := range list {
		if s.ID != "" {
			sessions[s.ID] = s
		}
	}
	return sessions, nil
}

// LaunchOptions describe one background session.
type LaunchOptions struct {
	Dir            string
	Name           string
	Prompt         string
	SystemPrompt   string
	Agent          string
	Model          string
	PermissionMode string
}

var backgrounded = regexp.MustCompile(`backgrounded\s+·\s+([0-9a-f]{6,})`)

// Launch starts a background session with `claude --bg` and returns its short
// ID. The prompt follows `--` so text starting with a dash is not a flag.
func Launch(ctx context.Context, o LaunchOptions) (string, error) {
	args := []string{"--bg", "--name", o.Name}
	if o.Agent != "" {
		args = append(args, "--agent", o.Agent)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	if o.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.SystemPrompt)
	}
	args = append(args, "--", o.Prompt)
	out, err := run(ctx, o.Dir, 2*time.Minute, args...)
	if err != nil {
		return "", err
	}
	m := backgrounded.FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("claude --bg did not report a session ID: %s", strings.TrimSpace(string(out)))
	}
	return string(m[1]), nil
}

var discardToken = regexp.MustCompile(`--discard-unpushed\s+(\S+)`)

// Remove deletes a background session with `claude rm`, which also removes the
// worktree it owns. Claude Code refuses when the worktree's commits exist on no
// remote; with discardUnpushed set, Remove then repeats the command with the
// confirmation token Claude Code printed. Only pass it once the commits are
// known to be merged elsewhere.
func Remove(ctx context.Context, id string, discardUnpushed bool) error {
	out, err := run(ctx, "", time.Minute, "rm", id)
	text := string(out)
	if err != nil {
		text = err.Error()
	}
	// The refusal carries the token whether or not the exit status reports it.
	m := discardToken.FindStringSubmatch(text)
	if m == nil {
		return err
	}
	if !discardUnpushed {
		return fmt.Errorf("claude rm %s refused: %s", id, strings.TrimSpace(text))
	}
	_, err = run(ctx, "", time.Minute, "rm", id, "--discard-unpushed", m[1])
	return err
}

func run(ctx context.Context, dir string, timeout time.Duration, args ...string) ([]byte, error) {
	bin, err := Binary()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
		return nil, fmt.Errorf("claude %s: %w: %s", args[0], err, detail)
	}
	// Some notices go to stderr; the session ID line may be on either stream.
	return append(stdout.Bytes(), stderr.Bytes()...), nil
}
