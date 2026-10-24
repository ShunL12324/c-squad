// Package codex runs tasks as Codex threads on Codex's shared app-server
// daemon: thread/start and turn/start to start, thread/read and the newest
// turn to observe, turn/steer or turn/start to message, the thread's turns
// to peek, thread/archive to remove, `codex resume` to take over.
package codex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// Name is the engine's name in the tasks table.
const Name = "codex"

func init() {
	engine.Register(Name, func(c engine.Config) engine.Engine { return New(c) })
}

// Engine is the Codex engine.
type Engine struct {
	cfg engine.Config
	// Version is the client version sent in initialize.
	Version string
	// Now is the clock for the young-turn rule.
	Now func() time.Time
}

// New returns a Codex engine.
func New(c engine.Config) *Engine { return &Engine{cfg: c, Now: time.Now} }

func (e *Engine) Name() string { return Name }
func (e *Engine) Mark() string { return "⬡" }

// CheckArgs validates the thread options and returns them in canonical form.
func (e *Engine) CheckArgs(args []string, _ string) ([]string, error) {
	o, err := ParseOptions(args)
	if err != nil {
		return nil, err
	}
	return o.Args(), nil
}

func textInput(text string) []any {
	return []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}
}

func messageID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "csquad-" + hex.EncodeToString(b)
}

// Start starts a thread in spec.Cwd with the worker rules as developer
// instructions, names it "T12 · title" and starts its first turn. A thread
// that cannot be named or started is archived again.
func (e *Engine) Start(ctx context.Context, spec engine.StartSpec) (engine.Started, error) {
	var res engine.Started
	o, err := ParseOptions(spec.Args)
	if err != nil {
		return res, err
	}
	c, err := e.dial(ctx)
	if err != nil {
		return res, err
	}
	defer c.close()
	params := map[string]any{
		"cwd":            spec.Cwd,
		"approvalPolicy": or(o.Approval, DefaultApproval),
		"sandbox":        or(o.Sandbox, DefaultSandbox),
	}
	if model := or(o.Model, spec.Model); model != "" {
		params["model"] = model
	}
	if spec.Rules != "" {
		params["developerInstructions"] = spec.Rules + fmt.Sprintf("\nYour task ID is T%d.\n", spec.ID)
	}
	if len(o.Config) > 0 {
		params["config"] = o.Config
	}
	var started struct {
		Thread Thread `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", params, &started); err != nil {
		return res, fmt.Errorf("launching %s failed: %v", spec.Name(), err)
	}
	id := started.Thread.ID
	if id == "" {
		return res, fmt.Errorf("launching %s failed: thread/start returned no thread id", spec.Name())
	}
	undo := func(err error) (engine.Started, error) {
		_ = c.call(context.Background(), "thread/archive", map[string]any{"threadId": id}, nil)
		return res, fmt.Errorf("launching %s failed: %v", spec.Name(), err)
	}
	if err := c.call(ctx, "thread/name/set", map[string]any{"threadId": id, "name": spec.Name()}, nil); err != nil {
		return undo(err)
	}
	turn := map[string]any{"threadId": id, "input": textInput(spec.Prompt), "clientUserMessageId": messageID()}
	if o.Effort != "" {
		turn["effort"] = o.Effort
	}
	if err := c.call(ctx, "turn/start", turn, nil); err != nil {
		return undo(err)
	}
	res.SessionID = id
	return res, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// notLoaded reports an error that means the daemon does not hold the thread.
func notLoaded(err error) bool {
	var r *RPCError
	if !errors.As(err, &r) {
		return false
	}
	m := strings.ToLower(r.Message)
	return strings.Contains(m, "not loaded") || strings.Contains(m, "not found") || strings.Contains(m, "no rollout") || strings.Contains(m, "invalid thread id")
}

func (e *Engine) read(ctx context.Context, c *conn, id string) (Thread, error) {
	var r struct {
		Thread Thread `json:"thread"`
	}
	err := c.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &r)
	return r.Thread, err
}

// turns returns up to limit turns, newest first, with the items view
// "summary" or "full"; cursor continues a previous page.
func (e *Engine) turns(ctx context.Context, c *conn, id string, limit int, view, cursor string) ([]Turn, string, error) {
	params := map[string]any{"threadId": id, "limit": limit, "sortDirection": "desc", "itemsView": view}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var r struct {
		Data       []Turn  `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := c.call(ctx, "thread/turns/list", params, &r); err != nil {
		return nil, "", err
	}
	next := ""
	if r.NextCursor != nil {
		next = *r.NextCursor
	}
	return r.Data, next, nil
}

func (e *Engine) newest(ctx context.Context, c *conn, id string) (*Turn, error) {
	list, _, err := e.turns(ctx, c, id, 1, "summary", "")
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// States observes threads over one connection: thread/read, and the newest
// turn unless the thread is active. A thread the daemon does not know reads
// as gone.
func (e *Engine) States(ctx context.Context, ids []string) (map[string]engine.Live, error) {
	out := map[string]engine.Live{}
	if len(ids) == 0 {
		return out, nil
	}
	c, err := e.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	for _, id := range ids {
		if _, ok := out[id]; ok {
			continue
		}
		l, err := e.state(ctx, c, id)
		if err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, nil
}

func (e *Engine) state(ctx context.Context, c *conn, id string) (engine.Live, error) {
	t, err := e.read(ctx, c, id)
	if notLoaded(err) {
		return engine.Live{State: engine.Gone}, nil
	}
	if err != nil {
		return engine.Live{}, err
	}
	var newest *Turn
	if t.Status.Type != "active" && t.Status.Type != "systemError" {
		if newest, err = e.newest(ctx, c, id); err != nil && !notLoaded(err) {
			return engine.Live{}, err
		}
	}
	// No Cwd: a thread's cwd is where it started, not the worktree it made.
	l := engine.Live{State: Live(t, newest, e.Now()), SessionID: or(t.ID, id)}
	if t.CreatedAt > 0 {
		l.StartedAt = time.Unix(t.CreatedAt, 0)
	}
	return l, nil
}

// Send adds text to the running turn (turn/steer with its id) or, when no
// turn runs, starts a new one, after thread/resume when the daemon unloaded
// the thread. A resumed thread runs with the user's Codex configuration.
func (e *Engine) Send(ctx context.Context, id, text string) (string, error) {
	c, err := e.dial(ctx)
	if err != nil {
		return "", err
	}
	defer c.close()
	t, err := e.read(ctx, c, id)
	if notLoaded(err) {
		return "", fmt.Errorf("Codex no longer has thread %s", id)
	}
	if err != nil {
		return "", err
	}
	input := textInput(text)
	if t.Status.Type == "active" {
		if turn, err := e.newest(ctx, c, id); err == nil && turn != nil && turn.Status == "inProgress" {
			err := c.call(ctx, "turn/steer", map[string]any{"threadId": id, "input": input, "expectedTurnId": turn.ID, "clientUserMessageId": messageID()}, nil)
			if err == nil {
				return "added to the running turn " + turn.ID, nil
			}
			// The turn ended meanwhile: start a new one below.
		}
	}
	how := "started a new turn"
	if t.Status.Type == "notLoaded" {
		if err := c.call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, nil); err != nil {
			return "", err
		}
		how = "resumed the thread and started a new turn"
	}
	var r struct {
		Turn Turn `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", map[string]any{"threadId": id, "input": input, "clientUserMessageId": messageID()}, &r); err != nil {
		return "", err
	}
	if r.Turn.ID != "" {
		how += " " + r.Turn.ID
	}
	return how, nil
}

// Last returns the last n entries of the thread (n <= 0: back to the newest
// assistant message), reading turns newest first.
func (e *Engine) Last(ctx context.Context, id string, n int) ([]engine.Message, error) {
	if id == "" {
		return nil, nil
	}
	c, err := e.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	var turns []Turn // oldest first
	cursor := ""
	for page := 0; page < 20; page++ {
		list, next, err := e.turns(ctx, c, id, 4, "full", cursor)
		if err != nil {
			return nil, err
		}
		for _, t := range list {
			turns = append([]Turn{t}, turns...)
		}
		msgs := Messages(turns)
		if next == "" || len(list) == 0 || n > 0 && len(msgs) > n || n <= 0 && engine.LastAssistant(msgs) != "" {
			if n > 0 && len(msgs) > n {
				msgs = msgs[len(msgs)-n:]
			}
			return msgs, nil
		}
		cursor = next
	}
	msgs := Messages(turns)
	if n > 0 && len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	return msgs, nil
}

// Stop interrupts the running turn, if any.
func (e *Engine) Stop(ctx context.Context, id string) error {
	c, err := e.dial(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	turn, err := e.newest(ctx, c, id)
	if err != nil || turn == nil || turn.Status != "inProgress" {
		return err
	}
	return c.call(ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn.ID}, nil)
}

// Remove archives the thread (its rollout moves to archived_sessions/).
// Worktrees are not the thread's: merged is not needed.
func (e *Engine) Remove(ctx context.Context, id string, _ []string) (string, error) {
	c, err := e.dial(ctx)
	if err != nil {
		return "", err
	}
	defer c.close()
	if err := c.call(ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		if notLoaded(err) {
			return "thread " + id + " was already removed", nil
		}
		return "", err
	}
	return "archived thread " + id, nil
}

// Takeover is the command that opens the thread in Codex.
func (e *Engine) Takeover(id string) []string { return []string{"codex", "resume", id} }
