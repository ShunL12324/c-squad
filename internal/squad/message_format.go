package squad

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

func messageBody(msg *Message, generation int, includeSender bool) string {
	fields := []string{"message_id=" + msg.ID}
	if includeSender {
		fields = append(fields, "from="+msg.From)
	}
	if msg.Task != "" {
		fields = append(fields, "task_id="+msg.Task)
	}
	if msg.ReplyTo != "" {
		fields = append(fields, "reply_to="+msg.ReplyTo)
	}
	fields = append(fields, fmt.Sprintf("recipient_generation=%d", generation))
	return "[C-Squad " + strings.Join(fields, " ") + "]\n" + msg.Text
}

// claudePeerFrame implements the native local peer envelope used by Claude Code
// 2.1.276. The socket frame and the rendered envelope must identify the same sender.
func claudePeerFrame(s *State, msg *Message, generation int) map[string]any {
	from := "did:csquad:" + url.PathEscape(s.ID) + ":" + url.PathEscape(msg.From)
	if sender := s.Members[msg.From]; sender != nil && sender.Peer != "" {
		from = "uds:" + url.PathEscape(sender.Peer)
	}
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) || strings.ContainsRune(`"<>`, r) {
			return -1
		}
		return r
	}, msg.From)
	// Escape envelope delimiters in message text, preserving ordinary code and prose.
	body := strings.NewReplacer("<cross-session-message", `<\cross-session-message`, "</cross-session-message", `<\/cross-session-message`).Replace(messageBody(msg, generation, false))
	content := fmt.Sprintf("<cross-session-message from=\"%s\" from-name=\"%s\">\n%s\n</cross-session-message>", from, name, body)
	return map[string]any{"type": "user", "from": from, "priority": "next", "message": map[string]string{"role": "user", "content": content}}
}
