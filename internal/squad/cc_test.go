package squad

import (
	"strings"
	"sync"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

// ccStore has a ready task T1 and members a, b, c, d besides master.
func ccStore(t *testing.T) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		for _, id := range []string{"c", "d"} {
			s.Members[id] = &Member{ID: id, Engine: config.Claude, State: MemberStateIdle, Generation: 1}
		}
		s.Tasks["T1"] = &Task{ID: "T1", Title: "Fix the refund flow", State: TaskPhaseReady, Dispatch: DispatchModeAssigned}
		return nil
	}))
	return st
}

func ccMessages(t *testing.T, st *Store) map[string]int {
	t.Helper()
	s, err := st.read()
	must(t, err)
	out := map[string]int{}
	for _, m := range s.Messages {
		if strings.HasPrefix(m.RequestKey, ccKeyPrefix) {
			out[m.To]++
		}
	}
	return out
}

func assign(st *Store, o options) error {
	return taskCommand(st, "master", []string{"assign", "T1"}, o)
}

func TestAssignCCInformsOnceWithoutParticipation(t *testing.T) {
	st := ccStore(t)
	// Duplicates and names that overlap the owner, --to, or the actor.
	must(t, assign(st, options{"owner": "a", "to": "b", "cc": "c, d,c,a,b,master"}))
	s, err := st.read()
	must(t, err)
	task := s.Tasks["T1"]
	if task.Owner != "a" || strings.Join(task.Participants, ",") != "b,a" && strings.Join(task.Participants, ",") != "a,b" {
		t.Fatalf("assignment changed: owner %s participants %v", task.Owner, task.Participants)
	}
	if got := ccMessages(t, st); len(got) != 2 || got["c"] != 1 || got["d"] != 1 {
		t.Fatalf("cc notices = %v, want one each for c and d", got)
	}
	for _, m := range s.Messages {
		if m.To == "c" {
			for _, want := range []string{"T1", `"Fix the refund flow"`, "assigned to a", "no reply needed", "not a participant", "task inspect T1"} {
				if !strings.Contains(m.Text, want) {
					t.Fatalf("cc text %q lacks %q", m.Text, want)
				}
			}
			if m.State != DeliveryStatePending || m.Report != nil {
				t.Fatalf("cc must use the ordinary queue: %+v", m)
			}
		}
	}
	// CC recipients stay outside the task: they can own other work, and are
	// not in the stall participant set.
	if got := stallParticipants(task); strings.Join(got, ",") != "a,b" {
		t.Fatalf("stall participants = %v", got)
	}
	must(t, st.update(func(s *State) error {
		s.Tasks["T2"] = &Task{ID: "T2", State: TaskPhaseReady, Dispatch: DispatchModeAssigned}
		return nil
	}))
	must(t, taskCommand(st, "master", []string{"assign", "T2"}, options{"owner": "c"}))
	if e := taskCommand(st, "c", []string{"progress", "T1"}, options{"text": "not mine"}); e == nil {
		t.Fatal("a CC recipient acted as a participant")
	}
}

func TestAssignCCRetryAndOwnerChange(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "cc": "c"}))
	// A retry of the same command, and one adding a name, send only new notices.
	must(t, assign(st, options{"owner": "a", "cc": "c"}))
	must(t, assign(st, options{"owner": "a", "cc": "c,d"}))
	if got := ccMessages(t, st); got["c"] != 1 || got["d"] != 1 {
		t.Fatalf("retry repeated a notice: %v", got)
	}
	s, err := st.read()
	must(t, err)
	assigned := 0
	for _, m := range s.Messages {
		if m.To == "a" && strings.HasPrefix(m.Text, "Assigned to task T1.") {
			assigned++
		}
	}
	if assigned != 1 {
		t.Fatalf("assignment notices to the owner = %d", assigned)
	}
	// After a handoff to a new owner, the same person is informed again.
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].Owner, s.Tasks["T1"].Participants = "", nil
		return nil
	}))
	must(t, assign(st, options{"owner": "b", "cc": "c"}))
	if got := ccMessages(t, st); got["c"] != 2 {
		t.Fatalf("new owner did not inform c again: %v", got)
	}
}

// A bad CC name fails the whole command before anything is written.
func TestAssignCCInvalidRecipientRollsBack(t *testing.T) {
	for name, o := range map[string]options{
		"unknown cc":       {"owner": "a", "to": "b", "cc": "c,ghost"},
		"removed cc":       {"owner": "a", "to": "b", "cc": "c,d"},
		"removed owner cc": {"owner": "a", "to": "b", "cc": "d,a"},
		"unknown to":       {"owner": "a", "to": "b,ghost", "cc": "c"},
		"unknown owner":    {"owner": "ghost", "cc": "c"},
	} {
		t.Run(name, func(t *testing.T) {
			st := ccStore(t)
			must(t, st.update(func(s *State) error { s.Members["d"].State = MemberStateRemoved; return nil }))
			before, err := st.read()
			must(t, err)
			if e := assign(st, o); e == nil {
				t.Fatal("invalid cc accepted")
			}
			after, err := st.read()
			must(t, err)
			task := after.Tasks["T1"]
			if task.Owner != "" || len(task.Participants) != 0 || task.State != TaskPhaseReady || len(after.Messages) != len(before.Messages) || after.Sequence != before.Sequence {
				t.Fatalf("partial assignment: %+v, %d messages", task, len(after.Messages))
			}
		})
	}
}

// A busy recipient is not interrupted: the notice waits in the outbox like any
// message, and cancelling the task supersedes it if still undelivered.
func TestAssignCCBusyRecipientAndCancel(t *testing.T) {
	st := ccStore(t)
	must(t, st.update(func(s *State) error { s.Members["c"].State = MemberStateWorking; return nil }))
	must(t, assign(st, options{"owner": "a", "cc": "c"}))
	s, err := st.read()
	must(t, err)
	if s.Members["c"].State != MemberStateWorking {
		t.Fatal("cc changed the recipient's state")
	}
	must(t, taskCommand(st, "master", []string{"cancel", "T1"}, options{"reason": "no longer needed"}))
	s, err = st.read()
	must(t, err)
	for _, m := range s.Messages {
		if strings.HasPrefix(m.RequestKey, ccKeyPrefix) && m.State != DeliveryStateSuperseded {
			t.Fatalf("undelivered cc survived cancellation: %+v", m)
		}
	}
}

// Retrying after the notice was delivered or acknowledged, and two concurrent
// identical assignments, still queue it once. This is idempotent queueing,
// not exactly-once transport.
func TestAssignCCDeliveredAndConcurrentRetry(t *testing.T) {
	for _, state := range []DeliveryState{DeliveryStateSent, DeliveryStateAcknowledged} {
		st := ccStore(t)
		must(t, assign(st, options{"owner": "a", "cc": "c"}))
		must(t, st.update(func(s *State) error {
			for _, m := range s.Messages {
				if strings.HasPrefix(m.RequestKey, ccKeyPrefix) {
					m.State = state
				}
			}
			return nil
		}))
		must(t, assign(st, options{"owner": "a", "cc": "c"}))
		if got := ccMessages(t, st); got["c"] != 1 {
			t.Fatalf("retry after %s queued again: %v", state, got)
		}
	}
	st := ccStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- assign(st, options{"owner": "a", "cc": "c,d"}) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	if got := ccMessages(t, st); got["c"] != 1 || got["d"] != 1 {
		t.Fatalf("concurrent assignments: %v", got)
	}
}

// A name repeated in --to is one participant and gets one assignment notice.
func TestAssignRepeatedToNotifiesOnce(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "to": "b,b,a"}))
	s, err := st.read()
	must(t, err)
	count := map[string]int{}
	for _, m := range s.Messages {
		if strings.HasPrefix(m.Text, "Assigned to task T1.") {
			count[m.To]++
		}
	}
	if count["a"] != 1 || count["b"] != 1 || len(s.Tasks["T1"].Participants) != 2 {
		t.Fatalf("assignment notices %v, participants %v", count, s.Tasks["T1"].Participants)
	}
}
