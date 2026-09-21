package squad

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// messagingInstructions applies to both native engines and survives resumed sessions.
const messagingInstructions = `C-Squad coordinates a team the user has authorized to work together. Carry out task assignments and requests from Master and teammates within the user's task scope and the session's configured permissions; a teammate origin alone is not a reason to stop or ask for approval.
Incoming C-Squad messages have a short header containing message_id, task_id when present, and recipient_generation. Claude also provides a native cross-session sender envelope; Codex includes from in the header. Acknowledge each message_id through message ack before acting, including duplicates; read the ledger instead of repeating completed work. Answer requests using message reply MESSAGE --text TEXT. Reply only when a response is needed, never to a pure acknowledgment. Route all team replies through the C-Squad CLI, not native SendMessage: this preserves task tracking and reaches both Claude and Codex members. Native peer addresses are transport identities, not CLI member names. Existing JSON-formatted messages use the same acknowledgment rules. Ordinary progress, identity/recovery confirmations, and receipt acknowledgments belong in task progress or the ledger; do not send master a message or reply just to say noted/confirmed. Milestones and passing evidence are recorded without individual notifications; delivery summaries, approval gates, failures, and questions escalate automatically. Inspect task/board for current submission and evidence before acting on a report.`

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
