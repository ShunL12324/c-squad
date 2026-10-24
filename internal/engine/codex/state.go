package codex

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// Thread is the part of the app-server's Thread csquad reads.
type Thread struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Cwd       string       `json:"cwd"`
	CreatedAt int64        `json:"createdAt"` // unix seconds
	UpdatedAt int64        `json:"updatedAt"`
	Path      string       `json:"path"`
	Status    ThreadStatus `json:"status"`
	Turns     []Turn       `json:"turns"`
}

// ThreadStatus: notLoaded | idle | systemError | active (with activeFlags).
type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

// Turn is one turn: completed | interrupted | failed | inProgress.
type Turn struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Items     []Item `json:"items"`
	StartedAt *int64 `json:"startedAt"` // unix seconds
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Item is one thread item; only messages are read, the rest count as tools.
type Item struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	Text      string            `json:"text"`      // agentMessage
	Content   []json.RawMessage `json:"content"`   // userMessage: UserInput entries
	Questions []json.RawMessage `json:"questions"` // agentMessage asking the user
	Tool      string            `json:"tool"`      // mcpToolCall, dynamicToolCall
}

// YoungTurn: a new turn first spends a few seconds (hooks, MCP servers)
// listed as interrupted without its user message while the thread reads
// idle; a turn like that younger than this reads as working.
var YoungTurn = 2 * time.Minute

// Live maps a thread and its newest turn (nil: none) to a session state.
func Live(t Thread, newest *Turn, now time.Time) string {
	switch t.Status.Type {
	case "active":
		for _, f := range t.Status.ActiveFlags {
			if f == "waitingOnApproval" || f == "waitingOnUserInput" {
				return engine.NeedsInput
			}
		}
		return engine.Working
	case "systemError":
		return engine.Failed
	}
	if newest == nil {
		return engine.Idle
	}
	switch newest.Status {
	case "inProgress":
		return engine.Working
	case "failed":
		return engine.Failed
	case "interrupted":
		if !hasUserMessage(*newest) {
			if at, ok := turnTime(*newest); ok && now.Sub(at) < YoungTurn {
				return engine.Working
			}
		}
		return engine.Stopped
	case "completed":
		if asks(*newest) {
			return engine.NeedsInput
		}
		return engine.Idle
	}
	return engine.Idle
}

func hasUserMessage(t Turn) bool {
	for _, it := range t.Items {
		if it.Type == "userMessage" {
			return true
		}
	}
	return false
}

// asks reports whether a completed turn ended with a question to the user:
// its final message carries questions, or its last line ends with "?".
func asks(t Turn) bool {
	for i := len(t.Items) - 1; i >= 0; i-- {
		it := t.Items[i]
		if it.Type != "agentMessage" {
			continue
		}
		if len(it.Questions) > 0 {
			return true
		}
		text := strings.TrimSpace(it.Text)
		if i := strings.LastIndex(text, "\n"); i >= 0 {
			text = strings.TrimSpace(text[i+1:])
		}
		text = strings.TrimRight(text, "*_` ")
		return strings.HasSuffix(text, "?") || strings.HasSuffix(text, "？")
	}
	return false
}

// turnTime is when a turn started: from its UUIDv7 id (the first 48 bits
// are unix milliseconds), else from startedAt.
func turnTime(t Turn) (time.Time, bool) {
	if at, ok := uuidTime(t.ID); ok {
		return at, true
	}
	if t.StartedAt != nil && *t.StartedAt > 0 {
		return time.Unix(*t.StartedAt, 0), true
	}
	return time.Time{}, false
}

func uuidTime(id string) (time.Time, bool) {
	h := strings.ReplaceAll(id, "-", "")
	if len(h) != 32 || h[12] != '7' {
		return time.Time{}, false
	}
	b, err := hex.DecodeString(h[:12])
	if err != nil {
		return time.Time{}, false
	}
	var ms int64
	for _, x := range b {
		ms = ms<<8 | int64(x)
	}
	return time.UnixMilli(ms), true
}

// userText is the text of a userMessage's inputs.
func userText(it Item) string {
	var parts []string
	for _, raw := range it.Content {
		var in struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		switch in.Type {
		case "text":
			parts = append(parts, in.Text)
		case "localImage", "image":
			parts = append(parts, "[image]")
		case "skill", "mention":
			parts = append(parts, "["+in.Name+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// skipped are items that are neither messages nor tool calls.
var skipped = map[string]bool{"reasoning": true, "plan": true, "hookPrompt": true, "contextCompaction": true,
	"enteredReviewMode": true, "exitedReviewMode": true, "functionCallOutput": true}

// Messages turns items (oldest first) into peek entries: user and assistant
// text, runs of tool calls folded into one entry with counts. Items carry no
// time of their own: a message is dated by the start of its turn.
func Messages(turns []Turn) []engine.Message {
	var out []engine.Message
	for _, t := range turns {
		at, _ := turnTime(t)
		for _, it := range t.Items {
			switch {
			case it.Type == "userMessage":
				if text := strings.TrimSpace(userText(it)); text != "" {
					out = append(out, engine.Message{Role: "user", Text: text, At: at})
				}
			case it.Type == "agentMessage":
				if text := strings.TrimSpace(it.Text); text != "" {
					out = append(out, engine.Message{Role: "assistant", Text: text, At: at})
				}
			case skipped[it.Type]:
			default:
				name := it.Type
				if it.Tool != "" {
					name = it.Tool
				}
				if len(out) == 0 || out[len(out)-1].Role != "tools" {
					out = append(out, engine.Message{Role: "tools"})
				}
				m := &out[len(out)-1]
				found := false
				for i := range m.Tools {
					if m.Tools[i].Name == name {
						m.Tools[i].Count++
						found = true
					}
				}
				if !found {
					m.Tools = append(m.Tools, engine.ToolCount{Name: name, Count: 1})
				}
			}
		}
	}
	return out
}
