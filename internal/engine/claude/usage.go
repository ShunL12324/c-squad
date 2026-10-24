package claude

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// A transcript has one usage per assistant message, and the same message
// appears on several lines (one per content block), so usage is counted by
// message id, the last line winning. The transcript is read incrementally:
// the state file keeps the offset reached, a checksum of the bytes before it
// (a rewritten file starts over) and the counts so far.

// recentIDs is how many of the newest messages stay open to a later line
// with the same id; older ones are folded into the total.
const recentIDs = 64

type counts struct {
	Input      int64 `json:"i"`
	CacheWrite int64 `json:"w"`
	Output     int64 `json:"o"`
	CacheRead  int64 `json:"r"`
}

func (c *counts) add(o counts) {
	c.Input += o.Input
	c.CacheWrite += o.CacheWrite
	c.Output += o.Output
	c.CacheRead += o.CacheRead
}

type msgUsage struct {
	ID string `json:"id"`
	U  counts `json:"u"`
}

// usageState is where the reading of a transcript stopped.
type usageState struct {
	Path   string     `json:"path"`
	Offset int64      `json:"offset"`
	Sum    string     `json:"sum"`    // of the sumSpan bytes before Offset
	Total  counts     `json:"total"`  // messages folded out of Recent
	Recent []msgUsage `json:"recent"` // the newest messages, oldest first
	Last   counts     `json:"last"`   // the newest message: the context size
	Known  bool       `json:"known"`
}

const sumSpan = 256

type usageLine struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			Output     int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (s *usageState) take(line []byte) {
	if !bytes.Contains(line, []byte(`"usage"`)) {
		return
	}
	var l usageLine
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.ID == "" || l.Message.Usage == nil {
		return
	}
	u := l.Message.Usage
	c := counts{Input: u.Input, CacheWrite: u.CacheWrite, Output: u.Output, CacheRead: u.CacheRead}
	s.Known, s.Last = true, c
	for i := len(s.Recent) - 1; i >= 0; i-- {
		if s.Recent[i].ID == l.Message.ID {
			s.Recent[i].U = c
			return
		}
	}
	s.Recent = append(s.Recent, msgUsage{l.Message.ID, c})
	if len(s.Recent) > recentIDs {
		s.Total.add(s.Recent[0].U)
		s.Recent = s.Recent[1:]
	}
}

func checksum(f *os.File, offset int64) string {
	start := max(0, offset-sumSpan)
	b := make([]byte, offset-start)
	if _, err := f.ReadAt(b, start); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

// Usage reads the session transcript's token usage from where the previous
// call (state) stopped. Activity is the transcript's last write.
func (e *Engine) Usage(ctx context.Context, id, state string) (engine.Usage, error) {
	path := TranscriptPath(e.claudeDir(), id)
	if path == "" {
		return engine.Usage{}, errors.New("no transcript yet")
	}
	f, err := os.Open(path)
	if err != nil {
		return engine.Usage{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return engine.Usage{}, err
	}
	var s usageState
	if b, err := os.ReadFile(state); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Path != path || s.Offset > info.Size() || s.Offset > 0 && checksum(f, s.Offset) != s.Sum {
		s = usageState{Path: path}
	}
	if _, err := f.Seek(s.Offset, io.SeekStart); err != nil {
		return engine.Usage{}, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	var readErr error
	for n := 0; ; n++ {
		if n%4096 == 0 && ctx.Err() != nil {
			readErr = ctx.Err() // the next call goes on from here
			break
		}
		line, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than the buffer: read it whole.
			rest, err2 := r.ReadBytes('\n')
			line, err = append(append([]byte(nil), line...), rest...), err2
		}
		if err != nil {
			break // EOF, or a line still being written: read it next time
		}
		s.Offset += int64(len(line))
		s.take(line)
	}
	s.Sum = checksum(f, s.Offset)
	if b, err := json.Marshal(s); err == nil && os.MkdirAll(filepath.Dir(state), 0o700) == nil {
		_ = os.WriteFile(state, b, 0o600)
	}
	if readErr != nil {
		return engine.Usage{}, readErr
	}
	total := s.Total
	for _, m := range s.Recent {
		total.add(m.U)
	}
	return engine.Usage{
		Known: s.Known, Input: total.Input, CacheWrite: total.CacheWrite, Output: total.Output, CacheRead: total.CacheRead,
		Context:  s.Last.Input + s.Last.CacheWrite + s.Last.CacheRead,
		Activity: info.ModTime(),
	}, nil
}
