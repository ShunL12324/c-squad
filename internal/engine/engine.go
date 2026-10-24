// Package engine is what csquad needs from a coding agent that runs tasks:
// start a named session, observe its state, message it, read it, remove it.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// Live states of a session, as observed; never stored.
const (
	Working    = "working"
	NeedsInput = "needs_input"
	Idle       = "idle"
	Done       = "done"
	Failed     = "failed"
	Stopped    = "stopped"
	Gone       = "gone" // the engine no longer lists the session
)

// Live is one observation of a session.
type Live struct {
	State     string
	SessionID string // the engine's full id (a Claude session UUID)
	Cwd       string
	StartedAt time.Time
}

// StartSpec is what Start needs.
type StartSpec struct {
	ID      int // 12 → "T12"
	Title   string
	Cwd     string
	Model   string
	Prompt  string
	Rules   string   // worker rules; "" with --no-worker-rules
	InPlace bool     // --isolation none
	Args    []string // engine arguments after `--`, as CheckArgs returned them
	// StopHook is a command line run when a turn of the session ends
	// (csquad hook ... claude-stop); engines with a global hook ignore it.
	StopHook string
	// Schema is a JSON schema the final answer must follow (a reviewer's
	// verdict); engines that cannot enforce one ignore it.
	Schema []byte
	// Fork is a session of this engine (a full id) the task starts as a fork
	// of, inheriting its conversation; "" starts a new one.
	Fork string
}

// Name is the session name of a task: "T12 · title".
func (s StartSpec) Name() string { return fmt.Sprintf("T%d · %s", s.ID, s.Title) }

// Started is what Start reports.
type Started struct {
	SessionID string
	Notes     []string // said to the user on stderr (never secrets)
}

// Message is one entry of a conversation: user or assistant text, or a run
// of tool calls folded into one entry.
type Message struct {
	Role  string // user | assistant | tools
	Text  string
	Tools []ToolCount
	At    time.Time // when it was written; zero when the engine does not say
}

// ToolCount is how often one tool ran in a run of tool calls.
type ToolCount struct {
	Name  string
	Count int
}

// LastAssistant is the newest assistant text of msgs.
func LastAssistant(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Text
		}
	}
	return ""
}

// Usage is a session's token usage as its engine recorded it. Fresh work is
// input + cache writes + output; cache reads are shown apart, never counted.
type Usage struct {
	Known      bool // the session recorded usage (a gateway may record none)
	Input      int64
	CacheWrite int64
	Output     int64
	CacheRead  int64
	Context    int64     // the newest request's size: what the context window holds
	ContextMax int64     // the context window; 0 = unknown
	Activity   time.Time // the session's last write; zero = unknown
}

// Fresh is the work the session did: input + cache writes + output.
func (u Usage) Fresh() int64 { return u.Input + u.CacheWrite + u.Output }

// Minus takes base off the counts (never below zero): a fork starts with
// its parent's history, usage included.
func (u Usage) Minus(base Usage) Usage {
	u.Input = max(0, u.Input-base.Input)
	u.CacheWrite = max(0, u.CacheWrite-base.CacheWrite)
	u.Output = max(0, u.Output-base.Output)
	u.CacheRead = max(0, u.CacheRead-base.CacheRead)
	return u
}

// UsageReader is an engine that reads a session's usage. state is a file the
// engine may keep its reading position in between calls.
type UsageReader interface {
	Usage(ctx context.Context, sessionID, state string) (Usage, error)
}

// SaveState writes v as JSON to a state file (mode 0600) through a
// temporary file, so a concurrent reader (the status line and a csquad
// command) never sees half of it.
func SaveState(file string, v any) {
	b, err := json.Marshal(v)
	if err != nil || os.MkdirAll(filepath.Dir(file), 0o700) != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, file) != nil {
		os.Remove(tmp)
	}
}

// Engine runs tasks.
type Engine interface {
	Name() string
	// Mark is the engine's glyph in the status line.
	Mark() string
	// CheckArgs validates the arguments after `--` before anything starts and
	// returns them normalised (paths made absolute against cwd).
	CheckArgs(args []string, cwd string) ([]string, error)
	Start(ctx context.Context, spec StartSpec) (Started, error)
	// States observes sessions; ids the engine does not list read as Gone.
	States(ctx context.Context, ids []string) (map[string]Live, error)
	// Send gives a session a follow-up instruction.
	Send(ctx context.Context, sessionID, text string) (string, error)
	// Last returns the conversation's last n entries (tools folded).
	Last(ctx context.Context, sessionID string, n int) ([]Message, error)
	// Stop interrupts a session, keeping its conversation.
	Stop(ctx context.Context, sessionID string) error
	// Remove deletes a session. merged lists the worktrees verified merged
	// and already removed; only for those may the engine discard work.
	Remove(ctx context.Context, sessionID string, merged []string) (string, error)
	// Takeover is the command the user runs to attach to the session.
	Takeover(sessionID string) []string
}

// Config is what engines are built from.
type Config struct {
	Home      string   // $CSQUAD_HOME
	ClaudeDir string   // $CLAUDE_CONFIG_DIR
	Env       proc.Env // the environment csquad runs in
	// NoStart: never start a service an engine needs (the Codex daemon);
	// for the status line, which must stay fast.
	NoStart bool
}

var factories = map[string]func(Config) Engine{}

// Register makes an engine available by name.
func Register(name string, f func(Config) Engine) { factories[name] = f }

// New returns the named engine.
func New(name string, c Config) (Engine, error) {
	f, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown engine %q (available: %v)", name, Names())
	}
	return f(c), nil
}

// Names lists the registered engines.
func Names() []string {
	var out []string
	for n := range factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
