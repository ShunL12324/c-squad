package squad

import (
	"fmt"
	"slices"
	"strings"
)

// ccKeyPrefix starts with a character member IDs cannot contain, so it never
// collides with a member's own actor:request-id keys.
const ccKeyPrefix = "#cc:"

func ccKey(t *Task, recipient string) string {
	return ccKeyPrefix + t.ID + ":" + t.Owner + ":" + recipient
}

func ccNotice(t *Task) string {
	return fmt.Sprintf("FYI, no reply needed: task %s %q is assigned to %s. You are copied for awareness only and are not a participant; run task inspect %s if it concerns you.",
		t.ID, reportExcerpt(t.Title, 120), t.Owner, t.ID)
}

// ccNotices informs members of an assignment once, without making them part
// of the task: they are not participants, owners, dependencies or watched for
// stalls. Participants (who got the assignment notice) and the actor are
// skipped. The stable key sends each recipient one notice per task owner, so
// retrying an assignment never repeats it.
func (s *State) ccNotices(actor string, t *Task, cc []string) {
	var sent []string
	for _, id := range cc {
		if id == actor || id == t.Owner || slices.Contains(t.Participants, id) || slices.Contains(sent, id) {
			continue
		}
		key := ccKey(t, id)
		if slices.ContainsFunc(s.Messages, func(m *Message) bool { return m.RequestKey == key }) {
			continue
		}
		m := s.message(actor, id, t.ID, ccNotice(t), "")
		m.RequestKey = key
		sent = append(sent, id)
	}
	if len(sent) > 0 {
		s.event(actor, "cc", t.ID+": "+strings.Join(sent, ","))
	}
}
