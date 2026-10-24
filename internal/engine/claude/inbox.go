package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A live Claude Code session listens on a Unix socket for messages from other
// sessions (what SendMessage uses). $CLAUDE_CONFIG_DIR/sessions/<pid>.json
// names it (messagingSocketPath); <pid>.<sha256 of the socket path>.key holds
// a peer token. The wire is newline-delimited JSON, one-shot: an optional
// {"type":"auth","token":...} line, then one {"type":"user",...} line, then
// close. Observed in Claude Code 2.1.294.
//
// csquad sends no sender envelope and asserts no permission mode. A task's
// settings accept cross-session messages (taskSettings), so Claude Code
// delivers them at once; a task whose --settings chose otherwise may hold the
// message until the user approves it in agent view.

type sessionFile struct {
	PID         int    `json:"pid"`
	SessionID   string `json:"sessionId"`
	Socket      string `json:"messagingSocketPath"`
	Spare       bool   `json:"spare"`
	ParkedJobID string `json:"parkedJobId"`
}

var pidJSON = regexp.MustCompile(`^\d+\.json$`)

// liveSocket returns the inbox socket of the live session full, or "".
func (e *Engine) liveSocket(full string) (string, int) {
	dir := filepath.Join(e.claudeDir(), "sessions")
	entries, _ := os.ReadDir(dir)
	for _, d := range entries {
		if !pidJSON.MatchString(d.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, d.Name()))
		if err != nil {
			continue
		}
		var s sessionFile
		if json.Unmarshal(b, &s) != nil || s.SessionID != full || s.Socket == "" || s.Spare || s.ParkedJobID != "" {
			continue
		}
		c, err := net.DialTimeout("unix", s.Socket, time.Second)
		if err != nil {
			continue // a record left by a session that is gone
		}
		c.Close()
		return s.Socket, s.PID
	}
	return "", 0
}

// hashOf names a socket's key file: sha256 of its absolute path, in hex.
func hashOf(socket string) string {
	abs, err := filepath.Abs(socket)
	if err != nil {
		abs = socket
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// peerToken reads the token for socket from its key file, or "".
func (e *Engine) peerToken(socket string, pid int) string {
	hash := hashOf(socket)
	b, err := os.ReadFile(filepath.Join(e.claudeDir(), "sessions", fmt.Sprintf("%d.%s.key", pid, hash)))
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(e.claudeDir(), "sessions", "*."+hash+".key"))
		if len(matches) == 0 {
			return ""
		}
		if b, err = os.ReadFile(matches[0]); err != nil {
			return ""
		}
	}
	var k struct {
		Token string `json:"peerToken"`
	}
	_ = json.Unmarshal(b, &k)
	return k.Token
}

// Send gives a session a follow-up instruction: into its inbox while it is
// live (working or idle), else by waking it with `claude --bg --resume`, which
// keeps the session's saved options (name, permission mode, settings,
// appended system prompt, model).
func (e *Engine) Send(ctx context.Context, id, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the message is empty")
	}
	list, err := e.Agents(ctx)
	if err != nil {
		return "", err
	}
	s, ok := match(list, id)
	if !ok {
		return "", fmt.Errorf("session %s is not listed by claude agents (removed)", id)
	}
	full := s.SessionID
	if full == "" {
		full = s.ID
	}
	if sock, pid := e.liveSocket(full); sock != "" {
		if err := deliver(sock, e.peerToken(sock, pid), full, text); err != nil {
			return "", fmt.Errorf("delivering to session %s: %v", id, err)
		}
		return "delivered to the inbox of session " + id + " (no receipt)", nil
	}
	if _, err := e.run(ctx, s.Cwd, "--bg", "--resume", full, "--", text); err != nil {
		return "", fmt.Errorf("resuming session %s: %v", id, err)
	}
	return "woke session " + id + " with the message", nil
}

func deliver(socket, token, session, text string) error {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var frames []byte
	if token != "" {
		b, _ := json.Marshal(map[string]string{"type": "auth", "token": token})
		frames = append(append(frames, b...), '\n')
	}
	b, err := json.Marshal(map[string]any{
		"type":       "user",
		"message":    map[string]string{"role": "user", "content": text},
		"priority":   "next",
		"session_id": session,
	})
	if err != nil {
		return err
	}
	if len(b) >= 1<<20 {
		return fmt.Errorf("the message is too long for the session inbox (1 MiB)")
	}
	frames = append(append(frames, b...), '\n')
	_, err = c.Write(frames)
	return err
}
