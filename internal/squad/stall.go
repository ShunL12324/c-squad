package squad

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

// stallWindow is how long every participant of an in-progress task must be
// observed quiet before master hears about a possible stall. One parameter,
// a first product value rather than a measured optimum.
var stallWindow = 5 * time.Minute

// stallGap is the longest pause between the starts of two runtime passes that
// still counts as continuous observation. A pass normally takes about two
// seconds; the headroom covers slow engine helpers with several members. A
// longer gap (suspend, a blocked runtime, a clock jump) proves nothing about
// the time in between, so every window restarts.
var stallGap = 2 * time.Minute

// stallKeyPrefix starts with a character member IDs cannot contain, so a
// member's own --request-id keys ("member:id:recipient") never collide.
const stallKeyPrefix = "#stall:"

// stallTimer remembers, in the runtime process only, when each task's current
// episode was first seen quiet. It is deliberately not persisted: a runtime
// restart, team stop or resume cannot then carry an idle period nobody
// observed. The notification itself is the durable record of an episode.
type stallTimer struct {
	since map[string]stallStart
	last  time.Time
}

type stallStart struct {
	fingerprint string
	at          time.Time
}

// stallParticipants is the task's owner and participants without duplicates.
func stallParticipants(t *Task) []string {
	ids := []string{}
	if t.Owner != "" {
		ids = append(ids, t.Owner)
	}
	for _, id := range t.Participants {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// stallFingerprint identifies one episode of the task: it changes with real
// ledger progress (Updated moves on progress, milestones, submit and reopen),
// the candidate, or a participant's incarnation, and never with heartbeats.
func stallFingerprint(s *State, t *Task) string {
	parts := []string{t.ID, string(t.State), t.Updated, t.Submission, t.Candidate}
	for _, id := range stallParticipants(t) {
		gen := 0
		if m := s.Members[id]; m != nil {
			gen = m.Generation
		}
		parts = append(parts, fmt.Sprintf("%s:%d", id, gen))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func stallKey(s *State, t *Task) string {
	return stallKeyPrefix + t.ID + ":" + stallFingerprint(s, t)
}

// quietMember reports whether a member is certainly doing nothing: finished a
// turn, failed, crashed, or stopped. Paused, waiting and starting members are
// accounted for elsewhere and never count as a stall.
func quietMember(m *Member) bool {
	switch m.State {
	case MemberStateIdle, MemberStateError, MemberStateCrashed, MemberStateStopped:
		return true
	}
	return false
}

// stallEligible holds when the ledger says the task should be progressing and
// nobody is known to be waiting: in progress, no blocker, gate or open
// question, every participant present and quiet, and nothing queued for them.
// known, when not nil, additionally requires every participant to have been
// observed this pass; the delivery-time check passes nil and trusts the ledger.
func stallEligible(s *State, t *Task, known map[string]bool) bool {
	if !s.Active || s.Phase != TeamPhaseRunning || t.State != TaskPhaseInProgress || len(t.Blockers) > 0 {
		return false
	}
	for _, ms := range t.Milestones {
		if ms.State == MilestoneStateAwaitingApproval {
			return false
		}
	}
	ids := stallParticipants(t)
	if len(ids) == 0 {
		return false
	}
	for _, q := range s.Questions {
		if q.State == QuestionStateOpen && (q.Task == t.ID || slices.Contains(ids, q.Member)) {
			return false
		}
	}
	for _, id := range ids {
		m := s.Members[id]
		if m == nil || m.State == MemberStateRemoved || !quietMember(m) {
			return false
		}
		if known != nil && !known[id] {
			return false
		}
	}
	// A queued message that can reach its recipient will wake it; that is not
	// a stall. Delivery to a crashed or stopped member waits for a restart, so
	// such a message cannot count as pending work.
	for _, msg := range s.Messages {
		if msg.State != DeliveryStatePending && msg.State != DeliveryStateSending || !slices.Contains(ids, msg.To) || msg.Report != nil && msg.Report.Kind == "stall" {
			continue
		}
		m := s.Members[msg.To]
		if deliveryPaused(m.State) || m.Engine == config.Claude && (m.EngineID == "" || m.Peer == "") {
			continue
		}
		return false
	}
	return true
}

// due advances the timers with one pass of observations and returns the tasks
// whose episode has been quiet for a whole window. known is nil when this
// pass observed nothing reliably, which restarts every window.
func (w *stallTimer) due(s *State, known map[string]bool, now time.Time) []string {
	if w.since == nil {
		w.since = map[string]stallStart{}
	}
	gap := !w.last.IsZero() && now.Sub(w.last) > stallGap || now.Before(w.last)
	w.last = now
	if gap || known == nil {
		clear(w.since)
		return nil
	}
	var out []string
	for id, t := range s.Tasks {
		if !stallEligible(s, t, known) {
			delete(w.since, id)
			continue
		}
		fp := stallFingerprint(s, t)
		start, ok := w.since[id]
		if !ok || start.fingerprint != fp {
			w.since[id] = stallStart{fp, now}
			continue
		}
		if now.Sub(start.at) >= stallWindow {
			out = append(out, id)
		}
	}
	for id := range w.since {
		if s.Tasks[id] == nil {
			delete(w.since, id)
		}
	}
	sort.Strings(out)
	return out
}

// notifyStall re-checks each due task inside one transaction and queues at
// most one notice per episode. The message's stable RequestKey is the
// deduplication record, written in the same update, so a crash either loses
// both or keeps both, and retries reuse the same message.
func (st *Store) notifyStall(ids []string, known map[string]bool) (bool, error) {
	queued := false
	err := st.update(func(s *State) error {
		for _, id := range ids {
			t := s.Tasks[id]
			if t == nil || !stallEligible(s, t, known) {
				continue
			}
			key := stallKey(s, t)
			exists := false
			for _, m := range s.Messages {
				if m.RequestKey == key {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			m := s.message("runtime", "master", t.ID, stallText(s, t), "")
			m.RequestKey = key
			m.Report = &ReportReference{Kind: "stall"}
			s.event("runtime", "stall_notice", t.ID)
			queued = true
		}
		return nil
	})
	return queued, err
}

func stallText(s *State, t *Task) string {
	var who []string
	for _, id := range stallParticipants(t) {
		who = append(who, id+" ("+string(s.Members[id].State)+")")
	}
	return fmt.Sprintf("Possible stall: task %s %q is in_progress, and no execution activity has been observed from %s for %s, with no gate, question or blocker recorded. Last task update: %s. Run task inspect %s and decide whether to wait, ask, or recover. This notice is sent once per episode; it does not mean the work failed or is complete, and background work the engines do not report can cause it.",
		t.ID, reportExcerpt(t.Title, 120), strings.Join(who, ", "), stallWindow, t.Updated, t.ID)
}

// stallCurrent keeps a stall notice deliverable only while its episode is
// unchanged and the task still looks stalled in the ledger.
func (s *State) stallCurrent(m *Message) bool {
	t := s.Tasks[m.Task]
	return t != nil && m.RequestKey == stallKey(s, t) && stallEligible(s, t, nil)
}

// checkStall runs once per runtime pass, after observation, on a fresh
// snapshot. A failed observation restarts every window.
func (st *Store) checkStall(w *stallTimer, known map[string]bool, observeErr error, now time.Time) error {
	if observeErr != nil {
		known = nil
	}
	s, err := st.read()
	if err != nil {
		return err
	}
	due := w.due(s, known, now)
	if len(due) == 0 {
		return nil
	}
	queued, err := st.notifyStall(due, known)
	if err == nil && queued {
		err = st.syncMessages()
	}
	return err
}
