package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// No app-server request returns a thread's token usage (it is pushed only to
// a subscribed client), so csquad reads the newest token_count event of the
// thread's rollout file, whose path thread/read gives once.

type rolloutState struct {
	Thread    string `json:"thread"`
	Path      string `json:"path"`
	UpdatedAt int64  `json:"updatedAt"` // unix seconds, from thread/read
}

type tokenUsage struct {
	Input      int64 `json:"input_tokens"` // cached input included
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Total      int64 `json:"total_tokens"`
}

type tokenInfo struct {
	Total  tokenUsage `json:"total_token_usage"`
	Last   tokenUsage `json:"last_token_usage"`
	Window int64      `json:"model_context_window"`
}

// Usage reads the thread's usage from its rollout file. Activity is the later
// of the thread's updatedAt and the rollout's last write.
func (e *Engine) Usage(ctx context.Context, id, state string) (engine.Usage, error) {
	var s rolloutState
	if b, err := os.ReadFile(state); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if _, err := os.Stat(s.Path); s.Thread != id || s.Path == "" || err != nil {
		c, err := e.dial(ctx)
		if err != nil {
			return engine.Usage{}, err
		}
		t, err := e.read(ctx, c, id)
		c.close()
		if err != nil {
			return engine.Usage{}, err
		}
		if t.Path == "" {
			return engine.Usage{}, errors.New("the thread has no rollout file")
		}
		s = rolloutState{Thread: id, Path: t.Path, UpdatedAt: t.UpdatedAt}
		engine.SaveState(state, s)
	}
	st, err := os.Stat(s.Path)
	if err != nil {
		return engine.Usage{}, err
	}
	u := engine.Usage{Activity: st.ModTime()}
	if at := time.Unix(s.UpdatedAt, 0); s.UpdatedAt > 0 && at.After(u.Activity) {
		u.Activity = at
	}
	info, ok, err := lastTokenCount(s.Path)
	if err != nil || !ok {
		return u, err
	}
	t := info.Total
	u.Known = true
	u.Input = max(0, t.Input-t.Cached-t.CacheWrite)
	u.CacheWrite, u.CacheRead, u.Output = t.CacheWrite, t.Cached, t.Output
	u.Context = info.Last.Total
	if u.Context == 0 {
		u.Context = info.Last.Input + info.Last.Output
	}
	u.ContextMax = info.Window
	return u, nil
}

// lastTokenCount finds the newest token_count event with usage, reading the
// file backwards (at most its last 16 MiB).
func lastTokenCount(path string) (tokenInfo, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return tokenInfo{}, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return tokenInfo{}, false, err
	}
	const chunk, limit = 64 << 10, 16 << 20
	size := st.Size()
	pos := size
	var tail []byte // from pos on, not yet looked at
	for pos > 0 && size-pos < limit {
		n := min(chunk, pos)
		pos -= n
		buf := make([]byte, n, n+int64(len(tail)))
		if _, err := f.ReadAt(buf, pos); err != nil {
			return tokenInfo{}, false, err
		}
		tail = append(buf, tail...)
		start := 0
		if pos > 0 {
			// The first line may continue in the previous chunk.
			i := bytes.IndexByte(tail, '\n')
			if i < 0 {
				continue
			}
			start = i + 1
		}
		lines := bytes.Split(tail[start:], []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			if info, ok := tokenCount(lines[i]); ok {
				return info, true, nil
			}
		}
		tail = tail[:start]
	}
	return tokenInfo{}, false, nil
}

func tokenCount(line []byte) (tokenInfo, bool) {
	if !bytes.Contains(line, []byte(`"token_count"`)) {
		return tokenInfo{}, false
	}
	var ev struct {
		Type    string `json:"type"`
		Payload struct {
			Type string     `json:"type"`
			Info *tokenInfo `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "event_msg" || ev.Payload.Type != "token_count" || ev.Payload.Info == nil {
		return tokenInfo{}, false
	}
	return *ev.Payload.Info, true
}
