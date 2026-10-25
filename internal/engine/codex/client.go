package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// The app-server speaks JSON-RPC 2.0 without the "jsonrpc" field, one
// message per WebSocket text frame. Its shared daemon listens on a Unix
// socket ($CODEX_HOME/app-server-control/app-server-control.sock), an
// AF_UNIX socket on Windows too. Observed with codex-cli 0.163.0.

// SocketRel is the control socket's path below $CODEX_HOME.
var SocketRel = filepath.Join("app-server-control", "app-server-control.sock")

// RPCError is an error answer of the app-server.
type RPCError struct {
	Method  string
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("codex %s: %s", e.Method, e.Message) }

// unreachable: nothing listens on the socket (missing, a dangling link,
// refused); starting the daemon may help.
type unreachable struct {
	path string
	err  error
}

func (e *unreachable) Error() string {
	return fmt.Sprintf("the Codex app-server is not reachable at %s: %v", e.path, e.err)
}
func (e *unreachable) Unwrap() error { return e.err }

// conn is one connection to the app-server; csquad opens one per command.
type conn struct {
	ws      *websocket.Conn
	next    atomic.Int64
	observe time.Duration // how long a read may take; see limit
	down    *down
}

// ObserveTimeout is how long the app-server may take to answer a read (the
// handshake, initialize, thread/read, thread/turns/list) or a removal; other
// requests start work and get a minute. A daemon that accepts the
// connection and never answers would otherwise hold csquad forever.
const ObserveTimeout = 8 * time.Second

func (c *conn) limit(method string) time.Duration {
	switch method {
	case "initialize", "thread/read", "thread/turns/list", "thread/archive", "turn/interrupt":
		return c.observe
	}
	return max(c.observe, time.Minute)
}

// notAnswering is the error of a request the app-server did not answer.
func notAnswering(detail string) *engine.NotAnswering {
	return &engine.NotAnswering{
		Service: "the Codex app-server",
		Detail:  detail,
		Short:   "codex app-server not answering",
		Fix:     "restart it: codex app-server daemon restart (it may interrupt running turns)",
	}
}

// envelope is any message: a response (id + result/error), a notification
// (method) or a request of the server (id + method).
type envelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// connect dials the socket at path (a link resolved first) and runs the
// WebSocket handshake and initialize.
func connect(ctx context.Context, path, version string, observe time.Duration) (*conn, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		if _, lerr := os.Lstat(path); lerr == nil && !errors.Is(err, os.ErrNotExist) {
			target = path // not a link csquad can read (a Windows socket): dial it as it is
		} else {
			return nil, &unreachable{path, err}
		}
	}
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", target)
	if err != nil {
		return nil, &unreachable{path, err}
	}
	used := false
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			if used {
				return nil, errors.New("one connection only")
			}
			used = true
			return nc, nil
		},
	}}
	hctx, cancel := context.WithTimeout(ctx, observe)
	defer cancel()
	ws, _, err := websocket.Dial(hctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		nc.Close()
		if errors.Is(hctx.Err(), context.DeadlineExceeded) {
			return nil, notAnswering(fmt.Sprintf("the WebSocket handshake got no answer in %s", observe))
		}
		return nil, fmt.Errorf("the Codex app-server at %s refused the connection: %v", path, err)
	}
	ws.SetReadLimit(256 << 20) // thread reads carry whole turns
	c := &conn{ws: ws, observe: observe}
	init := map[string]any{
		"clientInfo":   map[string]any{"name": "csquad", "title": "csquad", "version": version},
		"capabilities": map[string]any{"experimentalApi": true, "requestAttestation": false},
	}
	if err := c.call(ctx, "initialize", init, nil); err != nil {
		c.close()
		return nil, err
	}
	if err := c.write(ctx, envelope{Method: "initialized"}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *conn) close() { c.ws.Close(websocket.StatusNormalClosure, "") }

func (c *conn) write(ctx context.Context, m envelope) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// call sends one request and waits for its answer. Notifications and
// requests of the server (approvals, questions) are left alone: the daemon
// keeps such a request for a client that can answer it (`codex resume`).
func (c *conn) call(ctx context.Context, method string, params, result any) error {
	limit := c.limit(method)
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	err := c.exchange(ctx, method, params, result)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = notAnswering(fmt.Sprintf("%s got no answer in %s", method, limit))
		if c.down != nil {
			c.down.record(err)
		}
	}
	return err
}

// downFor: after a request got no answer, this process's next requests fail
// at once for this long instead of waiting again.
const downFor = 30 * time.Second

// down remembers a request the app-server did not answer.
type down struct {
	mu  sync.Mutex
	err *engine.NotAnswering
	at  time.Time
}

func (d *down) get() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil && time.Since(d.at) < downFor {
		return d.err
	}
	return nil
}

func (d *down) record(err error) {
	var na *engine.NotAnswering
	if errors.As(err, &na) {
		d.mu.Lock()
		d.err, d.at = na, time.Now()
		d.mu.Unlock()
	}
}

func (c *conn) exchange(ctx context.Context, method string, params, result any) error {
	id := c.next.Add(1)
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		raw = nil
	}
	if err := c.write(ctx, envelope{ID: json.RawMessage(fmt.Sprint(id)), Method: method, Params: raw}); err != nil {
		return fmt.Errorf("codex %s: %v", method, err)
	}
	want := fmt.Sprint(id)
	for {
		_, b, err := c.ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("codex %s: no answer: %v", method, err)
		}
		var m envelope
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Method != "" {
			continue
		}
		if string(bytes.TrimSpace(m.ID)) != want {
			continue
		}
		if m.Error != nil {
			m.Error.Method = method
			return m.Error
		}
		if result != nil && len(m.Result) > 0 {
			if err := json.Unmarshal(m.Result, result); err != nil {
				return fmt.Errorf("codex %s: unexpected answer: %v", method, err)
			}
		}
		return nil
	}
}

// codexHome is $CODEX_HOME or ~/.codex.
func codexHome(env proc.Env) string {
	if h := env.Get("CODEX_HOME"); h != "" {
		return h
	}
	home := env.Get("HOME")
	if home == "" || proc.Windows {
		if h := env.Get("USERPROFILE"); proc.Windows && h != "" {
			home = h
		} else if home == "" {
			home, _ = os.UserHomeDir()
		}
	}
	return filepath.Join(home, ".codex")
}

// dial connects to the daemon. When nothing listens it runs `codex
// app-server daemon start` (idempotent; it prints JSON with socketPath)
// and connects again, unless NoStart.
func (e *Engine) dial(ctx context.Context) (*conn, error) {
	if err := e.down.get(); err != nil {
		return nil, err
	}
	c, err := e.dialOnce(ctx)
	e.down.record(err)
	if c != nil {
		c.down = &e.down
	}
	return c, err
}

func (e *Engine) dialOnce(ctx context.Context) (*conn, error) {
	path := filepath.Join(codexHome(e.cfg.Env), SocketRel)
	c, err := connect(ctx, path, e.version(), e.observeTimeout())
	var u *unreachable
	if err == nil || !errors.As(err, &u) || e.cfg.NoStart {
		return c, err
	}
	started, serr := e.startDaemon(ctx)
	if serr != nil {
		return nil, fmt.Errorf("%v; starting it failed: %v", err, serr)
	}
	if started != "" {
		path = started
	}
	deadline := time.Now().Add(10 * time.Second)
	for wait := 50 * time.Millisecond; ; wait = min(2*wait, time.Second) {
		c, err = connect(ctx, path, e.version(), e.observeTimeout())
		if err == nil || !errors.As(err, &u) || time.Now().After(deadline) {
			return c, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// observeTimeout is ObserveTimeout, or CSQUAD_OBSERVE_TIMEOUT_MS (tests).
func (e *Engine) observeTimeout() time.Duration {
	if ms, err := strconv.Atoi(e.cfg.Env.Get("CSQUAD_OBSERVE_TIMEOUT_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return ObserveTimeout
}

func (e *Engine) version() string {
	if e.Version != "" {
		return e.Version
	}
	return "dev"
}

// startDaemon runs `codex app-server daemon start` and returns the socket
// path it reports ("" when it prints none).
func (e *Engine) startDaemon(ctx context.Context) (string, error) {
	out, err := e.run(ctx, "app-server", "daemon", "start")
	if err != nil {
		return "", err
	}
	var res struct {
		SocketPath string `json:"socketPath"`
	}
	if i := strings.Index(out, "{"); i >= 0 {
		_ = json.NewDecoder(strings.NewReader(out[i:])).Decode(&res)
	}
	return res.SocketPath, nil
}

// run runs codex with args and returns its stdout.
func (e *Engine) run(ctx context.Context, args ...string) (string, error) {
	c, err := proc.Resolve("codex", args, e.cfg.Env)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := c.Command(ctx)
	cmd.Env = e.cfg.Env
	proc.Bounded(cmd, time.Second)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("codex %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}
