// Package codextest is an in-process fake of Codex's app-server daemon for
// tests: a WebSocket server on a Unix socket in a temporary directory,
// speaking the JSON-RPC subset csquad uses, and a fake `codex` binary (the
// test binary itself) whose `app-server daemon start` links the control
// socket to it. Tests never start the real codex or touch ~/.codex.
package codextest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Thread is a fake thread.
type Thread struct {
	ID       string
	Name     string
	Cwd      string
	Created  time.Time
	Status   string   // idle | active | systemError (notLoaded while unloaded or archived)
	Flags    []string // activeFlags
	Turns    []*Turn
	Loaded   bool
	Archived bool
	Start    map[string]any // thread/start params
}

// Turn is a fake turn.
type Turn struct {
	ID     string
	Status string // inProgress | completed | interrupted | failed
	Items  []map[string]any
}

// Call is one request the fake answered.
type Call struct {
	Method string
	Params map[string]any
}

// Server is the fake app-server.
type Server struct {
	Home   string // $CODEX_HOME
	Socket string // where it listens (a short temporary path)
	// Link is the control socket path below Home; Listen links it to Socket.
	Link string

	mu      sync.Mutex
	threads map[string]*Thread
	order   []string
	calls   []Call
	fail    map[string]string // method → error message
	inits   int
	srv     *http.Server
}

// New starts a fake app-server for CODEX_HOME home. With linked, the
// control socket already points at it (a running daemon); otherwise only
// `codex app-server daemon start` links it.
func New(t testing.TB, home string, linked bool) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &Server{Home: home, Socket: filepath.Join(dir, "s.sock"), Link: filepath.Join(home, "app-server-control", "app-server-control.sock"),
		threads: map[string]*Thread{}, fail: map[string]string{}}
	ln, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve)}
	go s.srv.Serve(ln)
	t.Cleanup(func() { s.srv.Close() })
	if err := os.MkdirAll(filepath.Dir(s.Link), 0o700); err != nil {
		t.Fatal(err)
	}
	if linked {
		if err := LinkSocket(s.Link, s.Socket); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// LinkSocket points the control socket path at the listening socket,
// replacing whatever is there (a stale socket file).
func LinkSocket(link, socket string) error {
	os.Remove(link)
	return os.Symlink(socket, link)
}

// Env is what the fake `codex` needs to link the socket on daemon start.
func (s *Server) Env() []string {
	return []string{"CODEX_HOME=" + s.Home, "FAKE_CODEX_SOCKET=" + s.Socket, "FAKE_CODEX_LOG=" + filepath.Join(s.Home, "codex.argv")}
}

// DaemonStarts is how often the fake `codex app-server daemon start` ran.
func (s *Server) DaemonStarts() int {
	b, _ := os.ReadFile(filepath.Join(s.Home, "codex.argv"))
	return strings.Count(string(b), "app-server daemon start")
}

// Fail makes method answer with an error message ("" clears it).
func (s *Server) Fail(method, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if message == "" {
		delete(s.fail, method)
	} else {
		s.fail[method] = message
	}
}

// Calls returns the requests answered so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Methods returns the methods called so far, initialize excluded.
func (s *Server) Methods() []string {
	var out []string
	for _, c := range s.Calls() {
		if c.Method != "initialize" {
			out = append(out, c.Method)
		}
	}
	return out
}

// Last returns the params of the last call of method, nil when none.
func (s *Server) Last(method string) map[string]any {
	calls := s.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			return calls[i].Params
		}
	}
	return nil
}

// Reset forgets the recorded calls.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

// Thread returns a copy of a thread, nil when unknown.
func (s *Server) Thread(id string) *Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// Edit changes a thread under the lock.
func (s *Server) Edit(id string, f func(*Thread)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.threads[id]; t != nil {
		f(t)
	}
}

// Complete ends the newest turn with an agent message.
func (s *Server) Complete(id, text string) {
	s.Edit(id, func(t *Thread) {
		if n := len(t.Turns); n > 0 {
			turn := t.Turns[n-1]
			turn.Status = "completed"
			turn.Items = append(turn.Items, map[string]any{"type": "commandExecution", "id": NewID(time.Now())},
				map[string]any{"type": "agentMessage", "id": "msg_" + strconv.Itoa(len(turn.Items)), "text": text, "phase": "final_answer"})
		}
		t.Status, t.Flags = "idle", nil
	})
}

// Seed adds an idle thread with one completed turn (a dispatching thread).
func (s *Server) Seed(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.threads[id] = &Thread{ID: id, Created: now, Status: "idle", Loaded: true, Turns: []*Turn{{ID: NewID(now), Status: "completed",
		Items: []map[string]any{{"type": "agentMessage", "id": "msg_seed", "text": "earlier conversation"}}}}}
	s.order = append(s.order, id)
}

// Unload makes the daemon forget a loaded thread (as after a restart).
func (s *Server) Unload(id string) {
	s.Edit(id, func(t *Thread) { t.Loaded = false; t.Status = "idle" })
}

// NewID is a UUIDv7 for time at.
func NewID(at time.Time) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	ms := at.UnixMilli()
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = 0x70 | b[6]&0x0f
	b[8] = 0x80 | b[8]&0x3f
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type envelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 20)
	ctx := r.Context()
	initialized := false
	for {
		_, b, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m envelope
		if json.Unmarshal(b, &m) != nil {
			return
		}
		if bytesContain(b, `"jsonrpc"`) {
			s.answer(ctx, ws, m.ID, nil, errors.New("the app-server frames carry no jsonrpc field"))
			continue
		}
		if len(m.ID) == 0 {
			if m.Method == "initialized" {
				initialized = true
			}
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(m.Params, &params)
		if m.Method != "initialize" && !initialized {
			s.answer(ctx, ws, m.ID, nil, errors.New("Not initialized"))
			continue
		}
		res, err := s.handle(m.Method, params)
		s.answer(ctx, ws, m.ID, res, err)
	}
}

func bytesContain(b []byte, s string) bool { return strings.Contains(string(b), s) }

func (s *Server) answer(ctx context.Context, ws *websocket.Conn, id json.RawMessage, res any, err error) {
	// A notification first, as the daemon interleaves them.
	note, _ := json.Marshal(envelope{Method: "thread/status/changed", Params: json.RawMessage(`{}`)})
	_ = ws.Write(ctx, websocket.MessageText, note)
	e := envelope{ID: id}
	if err != nil {
		e.Error = &rpcError{Code: -32600, Message: err.Error()}
	} else {
		if res == nil {
			res = map[string]any{}
		}
		e.Result = res
	}
	b, _ := json.Marshal(e)
	_ = ws.Write(ctx, websocket.MessageText, b)
}

func str(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

func (s *Server) handle(method string, p map[string]any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{method, p})
	if msg := s.fail[method]; msg != "" {
		return nil, errors.New(msg)
	}
	id := str(p, "threadId")
	t := s.threads[id]
	needThread := func() error {
		if t == nil {
			return fmt.Errorf("thread not loaded: %s", id)
		}
		return nil
	}
	switch method {
	case "initialize":
		s.inits++
		return map[string]any{"userAgent": "fake", "codexHome": s.Home, "platformFamily": runtime.GOOS, "platformOs": runtime.GOOS}, nil
	case "thread/start":
		now := time.Now()
		t := &Thread{ID: NewID(now), Cwd: str(p, "cwd"), Created: now, Status: "idle", Loaded: true, Start: p}
		s.threads[t.ID] = t
		s.order = append(s.order, t.ID)
		return map[string]any{"thread": s.view(t, false)}, nil
	case "thread/fork":
		if t == nil || t.Archived {
			return nil, fmt.Errorf("no rollout found for thread id %s", id)
		}
		now := time.Now()
		f := &Thread{ID: NewID(now), Cwd: str(p, "cwd"), Created: now, Status: "idle", Loaded: true, Start: p}
		for _, turn := range t.Turns {
			c := *turn
			f.Turns = append(f.Turns, &c)
		}
		s.threads[f.ID] = f
		s.order = append(s.order, f.ID)
		return map[string]any{"thread": s.view(f, false)}, nil
	case "thread/name/set":
		if err := needThread(); err != nil {
			return nil, err
		}
		t.Name = str(p, "name")
		return nil, nil
	case "thread/read":
		if err := needThread(); err != nil {
			return nil, err
		}
		return map[string]any{"thread": s.view(t, p["includeTurns"] == true)}, nil
	case "thread/turns/list":
		if err := needThread(); err != nil {
			return nil, err
		}
		limit := 25
		if l, ok := p["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		start, _ := strconv.Atoi(str(p, "cursor"))
		var data []any
		for i := len(t.Turns) - 1 - start; i >= 0 && len(data) < limit; i-- {
			data = append(data, turnView(t.Turns[i], str(p, "itemsView")))
		}
		var next any
		if end := start + len(data); end < len(t.Turns) {
			next = strconv.Itoa(end)
		}
		if data == nil {
			data = []any{}
		}
		return map[string]any{"data": data, "nextCursor": next, "backwardsCursor": nil}, nil
	case "thread/resume":
		if t == nil || t.Archived {
			return nil, fmt.Errorf("no rollout found for thread id %s", id)
		}
		t.Loaded = true
		return map[string]any{"thread": s.view(t, false)}, nil
	case "turn/start":
		if t == nil || t.Archived {
			return nil, fmt.Errorf("thread not found: %s", id)
		}
		if !t.Loaded {
			return nil, fmt.Errorf("thread not loaded: %s", id)
		}
		turn := &Turn{ID: NewID(time.Now()), Status: "inProgress",
			Items: []map[string]any{{"type": "userMessage", "id": NewID(time.Now()), "clientId": p["clientUserMessageId"], "content": p["input"]}}}
		t.Turns = append(t.Turns, turn)
		t.Status = "active"
		return map[string]any{"turn": turnView(turn, "notLoaded")}, nil
	case "turn/steer":
		if err := needThread(); err != nil {
			return nil, err
		}
		n := len(t.Turns)
		if n == 0 || t.Turns[n-1].Status != "inProgress" || t.Turns[n-1].ID != str(p, "expectedTurnId") {
			return nil, errors.New("no active turn matching expectedTurnId")
		}
		t.Turns[n-1].Items = append(t.Turns[n-1].Items, map[string]any{"type": "userMessage", "id": NewID(time.Now()), "content": p["input"]})
		return map[string]any{"turnId": t.Turns[n-1].ID}, nil
	case "turn/interrupt":
		if err := needThread(); err != nil {
			return nil, err
		}
		for _, turn := range t.Turns {
			if turn.ID == str(p, "turnId") {
				turn.Status = "interrupted"
				t.Status = "idle"
				return nil, nil
			}
		}
		return nil, errors.New("no such turn")
	case "thread/archive":
		if t == nil || t.Archived {
			return nil, fmt.Errorf("no rollout found for thread id %s", id)
		}
		t.Archived, t.Loaded = true, false
		return nil, nil
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func (s *Server) view(t *Thread, withTurns bool) map[string]any {
	status := map[string]any{"type": t.Status}
	if !t.Loaded {
		status = map[string]any{"type": "notLoaded"}
	} else if t.Status == "active" {
		flags := t.Flags
		if flags == nil {
			flags = []string{}
		}
		status["activeFlags"] = flags
	}
	turns := []any{}
	if withTurns {
		for _, turn := range t.Turns {
			turns = append(turns, turnView(turn, "full"))
		}
	}
	var name any
	if t.Name != "" {
		name = t.Name
	}
	return map[string]any{"id": t.ID, "name": name, "cwd": t.Cwd, "createdAt": t.Created.Unix(), "updatedAt": t.Created.Unix(),
		"status": status, "turns": turns, "path": filepath.Join(s.Home, "sessions", t.ID+".jsonl")}
}

func turnView(t *Turn, items string) map[string]any {
	list := []any{}
	switch items {
	case "full":
		for _, it := range t.Items {
			list = append(list, it)
		}
	case "summary": // the user message and the final answer
		for _, it := range t.Items {
			if it["type"] == "userMessage" || it["type"] == "agentMessage" {
				list = append(list, it)
			}
		}
	}
	return map[string]any{"id": t.ID, "status": t.Status, "items": list, "itemsView": items, "startedAt": nil}
}
