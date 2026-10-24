package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// TranscriptPath finds the transcript of a session (full or short id): the
// newest projects/*/<id>.jsonl.
func TranscriptPath(claudeDir, id string) string {
	if id == "" {
		return ""
	}
	root := filepath.Join(claudeDir, "projects")
	dirs, _ := os.ReadDir(root)
	var best string
	var bestTime time.Time
	consider := func(f string) {
		if st, err := os.Stat(f); err == nil && (best == "" || st.ModTime().After(bestTime)) {
			best, bestTime = f, st.ModTime()
		}
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		consider(filepath.Join(root, d.Name(), id+".jsonl"))
		if best == "" && !strings.Contains(id, "-") {
			// A short id: the transcript is named by the full session id.
			matches, _ := filepath.Glob(filepath.Join(root, d.Name(), id+"-*.jsonl"))
			for _, m := range matches {
				consider(m)
			}
		}
	}
	return best
}

// Activity is the time of the session transcript's last write.
func (e *Engine) Activity(id string) (time.Time, bool) {
	f := TranscriptPath(e.claudeDir(), id)
	if f == "" {
		return time.Time{}, false
	}
	st, err := os.Stat(f)
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

var (
	reminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	wrapper  = regexp.MustCompile(`^<[a-z][a-z0-9-]*>`)
)

type entry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// Messages returns a transcript without tool traffic: user and assistant
// text, with runs of tool calls folded into one "tools" entry.
func Messages(r io.Reader) []engine.Message {
	var out []engine.Message
	tools := -1
	sc := bufio.NewReader(r)
	for {
		line, err := sc.ReadBytes('\n')
		if len(line) > 0 {
			var e entry
			if json.Unmarshal(line, &e) == nil && (e.Type == "user" || e.Type == "assistant") && !e.IsSidechain && !e.IsMeta {
				var blocks []block
				var s string
				if json.Unmarshal(e.Message.Content, &s) == nil {
					blocks = []block{{Type: "text", Text: s}}
				} else {
					_ = json.Unmarshal(e.Message.Content, &blocks)
				}
				for _, b := range blocks {
					switch b.Type {
					case "tool_use":
						if tools < 0 {
							out = append(out, engine.Message{Role: "tools"})
							tools = len(out) - 1
						}
						m := &out[tools]
						found := false
						for i := range m.Tools {
							if m.Tools[i].Name == b.Name {
								m.Tools[i].Count++
								found = true
							}
						}
						if !found {
							m.Tools = append(m.Tools, engine.ToolCount{Name: b.Name, Count: 1})
						}
					case "text":
						text := strings.TrimSpace(reminder.ReplaceAllString(b.Text, ""))
						// Harness wrappers such as <command-name> or <task-notification>.
						if text == "" || wrapper.MatchString(text) {
							continue
						}
						tools = -1
						out = append(out, engine.Message{Role: e.Type, Text: text})
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return out
}

// Last returns the last n entries of a session's conversation.
func (e *Engine) Last(_ context.Context, id string, n int) ([]engine.Message, error) {
	f := TranscriptPath(e.claudeDir(), id)
	if f == "" {
		return nil, nil
	}
	file, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	msgs := Messages(file)
	if n > 0 && len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	return msgs, nil
}

// LastAssistant is the newest assistant text of msgs.
func LastAssistant(msgs []engine.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Text
		}
	}
	return ""
}
