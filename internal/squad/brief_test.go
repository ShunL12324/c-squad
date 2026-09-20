package squad

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func briefStore(t *testing.T, phase TaskPhase) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", Title: "Wire the panel", Owner: "a", State: phase, Updated: now(),
			Participants: []string{"a"}, Milestones: []Milestone{}, Evidence: []Evidence{}}
		return nil
	}))
	return st
}

func taskJSON(t *testing.T, st *Store, id string) string {
	t.Helper()
	s, e := st.read()
	must(t, e)
	b, e := json.Marshal(s.Tasks[id])
	must(t, e)
	return string(b)
}

// Asking about a task must not touch it. Anything else would make the button a
// state change wearing a question mark.
func TestBriefLeavesTheTaskByteIdentical(t *testing.T) {
	for _, phase := range []TaskPhase{TaskPhaseInProgress, TaskPhaseInReview, TaskPhaseDone} {
		t.Run(string(phase), func(t *testing.T) {
			st := briefStore(t, phase)
			before := taskJSON(t, st, "T1")
			if _, e := briefEnqueue(st, "T1"); e != nil {
				t.Fatalf("brief on a %s task: %v", phase, e)
			}
			if after := taskJSON(t, st, "T1"); after != before {
				t.Fatalf("task changed\n before %s\n  after %s", before, after)
			}
		})
	}
}

// A done task is exactly the one a user most wants explained, and the phase
// guard rejects every operation after merge. The brief has to get past it.
func TestBriefReachesDoneTaskThroughTaskCommand(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	must(t, st.update(func(s *State) error {
		// Stop master so the only remaining refusal is a known one, reached
		// well after the phase guard would have fired.
		s.Members["master"].State = MemberStateStopped
		return nil
	}))
	if e := taskCommand(st, "master", []string{"progress", "T1"}, options{"text": "x"}); e == nil || !strings.Contains(e.Error(), "completed") {
		t.Fatalf("precondition: the phase guard no longer rejects edits to a done task: %v", e)
	}
	e := taskCommand(st, "master", []string{"brief", "T1"}, options{})
	if e == nil {
		t.Fatal("expected the absent master to be reported")
	}
	if !strings.Contains(e.Error(), "no master session") {
		t.Fatalf("the brief did not reach a done task; it failed with %v", e)
	}

	must(t, st.update(func(s *State) error {
		s.Members["master"].State = MemberStateIdle
		return nil
	}))
	if _, e := briefEnqueue(st, "T1"); e != nil {
		t.Fatalf("brief must record a request for a done task: %v", e)
	}
}

// One press, one request. tmux forwards a double click to the panel as several
// presses, and the runtime may retry, so the ledger has to be the guard.
func TestBriefKeyAdmitsOneLiveRequest(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	first, e := briefEnqueue(st, "T1")
	must(t, e)
	second, e := briefEnqueue(st, "T1")
	must(t, e)
	if first != second {
		t.Fatalf("second press queued %s alongside %s", second, first)
	}
	s, e := st.read()
	must(t, e)
	if len(s.Messages) != 1 {
		t.Fatalf("messages = %d, want one live request", len(s.Messages))
	}

	// A retry is the same request: attempts reset so the outbox tries again.
	must(t, st.update(func(s *State) error {
		s.Messages[0].State = DeliveryStateNeedsAttention
		s.Messages[0].Attempts = 3
		s.Messages[0].Error = "stalled"
		return nil
	}))
	retry, e := briefEnqueue(st, "T1")
	must(t, e)
	s, e = st.read()
	must(t, e)
	if retry != first || len(s.Messages) != 1 {
		t.Fatalf("retry queued a new message: %s, total %d", retry, len(s.Messages))
	}
	if s.Messages[0].State != DeliveryStatePending || s.Messages[0].Attempts != 0 || s.Messages[0].Error != "" {
		t.Fatalf("retry did not reset delivery: %+v", s.Messages[0])
	}

	// Once master has answered, the user may ask again.
	must(t, st.update(func(s *State) error {
		s.Messages[0].State = DeliveryStateAcknowledged
		return nil
	}))
	again, e := briefEnqueue(st, "T1")
	must(t, e)
	s, e = st.read()
	must(t, e)
	if again == first || len(s.Messages) != 2 {
		t.Fatalf("a fresh question after an answer reused %s; messages = %d", again, len(s.Messages))
	}
	for _, msg := range s.Messages {
		if msg.ID == first && msg.RequestKey != "" {
			t.Fatal("the answered request kept the key, so the next press would find it forever")
		}
	}
}

// The request says what it is and what it is not: an agent must not read a
// status question as licence to act on the task.
func TestBriefTextRefusesToAuthoriseAction(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	must(t, func() error { _, e := briefEnqueue(st, "T1"); return e }())
	s, e := st.read()
	must(t, e)
	msg := s.Messages[0]
	if msg.From != UserSender || msg.To != "master" || msg.Task != "T1" {
		t.Fatalf("wrong routing: %+v", msg)
	}
	for _, want := range []string{"changes no task state", "Do NOT approve, reopen, merge", "do not reply to this message through the CLI", s.ID, "T1"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("request text is missing %q:\n%s", want, msg.Text)
		}
	}
}

// An absent master must be reported, not swallowed: a request nobody will ever
// collect is worse than a refusal.
func TestBriefRefusesWhenMasterIsGone(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	must(t, st.update(func(s *State) error {
		s.Members["master"].State = MemberStateStopped
		return nil
	}))
	note, e := briefRequest(st, "master", "T1")
	if e == nil {
		t.Fatalf("expected a visible refusal, got note %q", note)
	}
	s, e2 := st.read()
	must(t, e2)
	if len(s.Messages) != 0 {
		t.Fatalf("refused request still queued %d message(s)", len(s.Messages))
	}
}

func TestBriefIsMasterOnly(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	if _, e := briefRequest(st, "a", "T1"); e == nil {
		t.Fatal("a member must not raise a request that speaks for the user")
	}
	if e := taskCommand(st, "a", []string{"brief", "T1"}, options{}); e == nil {
		t.Fatal("task brief must be master-only through the CLI too")
	}
}

// Delivery resolves the recipient generation when it runs, so a request raised
// before master restarts reaches the incarnation that exists afterwards.
func TestBriefDeliveredToTheCurrentMasterGeneration(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	sock := filepath.Join(t.TempDir(), "master.sock")
	id, e := briefEnqueue(st, "T1")
	must(t, e)

	// Master restarts: a new generation, and its queued mail is reset for it.
	must(t, st.update(func(s *State) error {
		m := s.Members["master"]
		m.Generation = 7
		m.EngineID = "master-7"
		m.Peer = sock
		for _, msg := range s.Messages {
			msg.resetDelivery()
		}
		return nil
	}))
	l, e := net.Listen("unix", sock)
	must(t, e)
	defer l.Close()
	received := make(chan string, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var v map[string]any
		json.NewDecoder(c).Decode(&v)
		b, _ := json.Marshal(v)
		received <- string(b)
	}()
	must(t, st.deliver(id))
	select {
	case frame := <-received:
		if !strings.Contains(frame, "recipient_generation=7") {
			t.Fatalf("frame does not carry the current generation: %s", frame)
		}
		if !strings.Contains(frame, `from-name=\"`+UserSender+`\"`) {
			t.Fatalf("frame does not name the user as the origin: %s", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery")
	}
	s, e := st.read()
	must(t, e)
	if s.Messages[0].RecipientGeneration != 7 {
		t.Fatalf("recorded generation = %d, want 7", s.Messages[0].RecipientGeneration)
	}
}

// The panel is rebuilt on every tmux layout pass, so what the user asked for has
// to be readable from the ledger rather than held in the process.
func TestBriefProjectionSurvivesPanelRespawn(t *testing.T) {
	st := briefStore(t, TaskPhaseInProgress)
	s, e := st.read()
	must(t, e)
	if got := briefFor(s, "T1"); got.MessageID != "" {
		t.Fatalf("unasked task already shows a request: %+v", got)
	}
	id, e := briefEnqueue(st, "T1")
	must(t, e)
	s, e = st.read()
	must(t, e)
	got := briefFor(s, "T1")
	if got.MessageID != id || got.State != string(DeliveryStatePending) {
		t.Fatalf("projection = %+v, want the live request %s", got, id)
	}
	must(t, st.update(func(s *State) error {
		s.Messages[0].State = DeliveryStateAcknowledged
		return nil
	}))
	s, e = st.read()
	must(t, e)
	if got = briefFor(s, "T1"); got.MessageID != "" {
		t.Fatalf("an answered request still shows as outstanding: %+v", got)
	}
}
