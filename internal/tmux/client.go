package tmux

import (
	"fmt"
	"strings"

	"github.com/ShunL12324/c-squad/internal/process"
)

// Client addresses one tmux server by socket path; it owns no server lifecycle.
type Client struct{ Socket string }

// Separator is the only argument Run passes to tmux as a command separator.
const Separator = ";"

// Run executes tmux with exact session resolution for targets prefixed with "=".
// This avoids tmux prefix matching accidentally targeting another team session.
// The caller's argument slice is never modified.
func (c Client) Run(args ...string) (string, error) {
	args = append([]string(nil), args...)
	for i, arg := range args {
		args[i] = literal(arg)
	}
	// tmux commands differ in how they interpret '=' and session/window
	// targets. Resolve exact names ourselves, then use the unambiguous $ID.
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-t" || args[i+1] == "=" || !strings.HasPrefix(args[i+1], "=") {
			continue
		}
		target := strings.TrimPrefix(args[i+1], "=")
		window := strings.HasSuffix(target, ":")
		target = strings.TrimSuffix(target, ":")
		sessions, e := process.Run("", "tmux", "-S", c.Socket, "list-sessions", "-F", "#{session_id} #{session_name}")
		if e != nil {
			return "", e
		}
		found := ""
		for _, line := range strings.Split(sessions, "\n") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 && parts[1] == target {
				found = parts[0]
				break
			}
		}
		if found == "" {
			return "", fmt.Errorf("no exact tmux session %s", target)
		}
		if window {
			found += ":"
		}
		args[i+1] = found
	}
	return process.Run("", "tmux", append([]string{"-S", c.Socket}, args...)...)
}

// literal keeps a trailing semicolon inside its argument. tmux splits argv
// into commands at any argument ending in ";" and reads a trailing "\;" as
// one literal ";", so a message, path or value ending in ";" or "\;" lost a
// character or became a second tmux command. Only a bare Separator splits.
func literal(arg string) string {
	if arg == Separator || !strings.HasSuffix(arg, ";") {
		return arg
	}
	return strings.TrimSuffix(arg, ";") + `\;`
}
