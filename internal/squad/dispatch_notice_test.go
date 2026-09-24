package squad

import (
	"strings"
	"sync/atomic"
	"testing"
)

// A member told about a task already knows its ID, so the notice points at
// task inspect, which returns that task's workspace and acceptance, rather than
// at the whole-team board.
func TestTaskNoticesPointAtTaskInspect(t *testing.T) {
	st := testStore(t)
	must(t, taskCommand(st, "master", []string{"create", "assigned work"}, options{"acceptance": "done"}))
	must(t, taskCommand(st, "master", []string{"create", "open work"}, options{"acceptance": "done", "dispatch": "open"}))
	s, err := st.read()
	must(t, err)
	ids := map[string]string{}
	for id, task := range s.Tasks {
		ids[task.Title] = id
	}
	assigned := ids["assigned work"]
	must(t, taskCommand(st, "master", []string{"assign", assigned}, options{"owner": "a"}))
	s, err = st.read()
	must(t, err)
	want := map[string]string{
		"a": "Assigned to task " + assigned + ". Run task inspect " + assigned + " ",
		"b": "Task available: " + ids["open work"] + " open work. Run task inspect " + ids["open work"] + " and claim if suitable.",
	}
	for _, m := range s.Messages {
		if prefix, ok := want[m.To]; ok && strings.HasPrefix(m.Text, prefix) {
			if strings.Contains(m.Text, "Read board") {
				t.Fatalf("notice still sends %s to the board: %q", m.To, m.Text)
			}
			delete(want, m.To)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing notices %v in %+v", want, s.Messages)
	}
}

func messageFor(t *testing.T, st *Store, kind, to string) *Message {
	t.Helper()
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.Report != nil && m.Report.Kind == kind && m.To == to {
			return m
		}
	}
	t.Fatalf("no %s notice to %s", kind, to)
	return nil
}

func TestAvailableNoticeExpiresAfterClaim(t *testing.T) {
	st := testStore(t)
	must(t, taskCommand(st, "master", []string{"create", "Open work"}, options{"acceptance": "done", "dispatch": "open"}))
	notice := messageFor(t, st, "available", "b")
	must(t, taskCommand(st, "a", []string{"claim", "T1"}, options{}))
	must(t, st.deliver(notice.ID))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.Report != nil && m.Report.Kind == "available" && (m.State != DeliveryStateSuperseded || m.Attempts != 0) {
			t.Fatalf("claimed task still woke %s: %+v", m.To, m)
		}
	}
}

func TestDispatchNoticeFreshnessAndHumanMessage(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "to": "b", "cc": "c"}))
	assigned := messageFor(t, st, "assigned", "b")
	cc := messageFor(t, st, "cc", "c")
	var received atomic.Int32
	peer := peerInbox(t, &received)
	var personal string
	must(t, st.update(func(s *State) error {
		s.Members["b"].Peer, s.Members["b"].EngineID = peer, "thread"
		personal = s.message("a", "b", "T1", "Please check the issue before closing", "").ID
		s.Tasks["T1"].State = TaskPhaseDone
		return nil
	}))
	for _, id := range []string{assigned.ID, cc.ID} {
		must(t, st.deliver(id))
	}
	must(t, st.deliver(personal))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == assigned.ID || m.ID == cc.ID {
			if m.State != DeliveryStateSuperseded || m.Attempts != 0 {
				t.Fatalf("finished task dispatched stale notice: %+v", m)
			}
		}
		if m.ID == personal && (m.State != DeliveryStateSent || !strings.Contains(m.Text, "Please check")) {
			t.Fatalf("member message was suppressed: %+v", m)
		}
	}
}

func TestCurrentAssignmentNoticeDelivers(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "to": "b"}))
	notice := messageFor(t, st, "assigned", "b")
	var received atomic.Int32
	peer := peerInbox(t, &received)
	must(t, st.update(func(s *State) error {
		s.Members["b"].Peer, s.Members["b"].EngineID = peer, "thread"
		return nil
	}))
	must(t, st.deliver(notice.ID))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == notice.ID && (m.State != DeliveryStateSent || m.Attempts != 1 || !strings.HasPrefix(m.Text, "Assigned to task T1.")) {
			t.Fatalf("current assignment was not delivered: %+v", m)
		}
	}
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].State = TaskPhaseDone
		s.expireReports()
		return nil
	}))
	s, err = st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == notice.ID && m.State != DeliveryStateSent {
			t.Fatalf("delivered assignment lost its transport history: %+v", m)
		}
	}
}

func TestOldOwnerCCExpiresAfterHandoff(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "cc": "c"}))
	old := messageFor(t, st, "cc", "c")
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].Owner = ""
		s.Tasks["T1"].Participants = nil
		return nil
	}))
	must(t, assign(st, options{"owner": "b", "cc": "c"}))
	must(t, st.deliver(old.ID))
	s, err := st.read()
	must(t, err)
	var expired bool
	var current bool
	for _, m := range s.Messages {
		if m.ID == old.ID {
			expired = m.State == DeliveryStateSuperseded && m.Attempts == 0
		}
		if m.Report != nil && m.Report.Kind == "cc" && m.Report.Owner == "b" {
			current = m.State == DeliveryStatePending
		}
	}
	if !expired {
		t.Fatal("old owner CC was not superseded")
	}
	if !current {
		t.Fatal("current owner's CC was suppressed")
	}
}

func TestReviewerAssignmentSurvivesOwnerHandoff(t *testing.T) {
	st := ccStore(t)
	must(t, assign(st, options{"owner": "a", "to": "b"}))
	reviewer := messageFor(t, st, "assigned", "b")
	owner := messageFor(t, st, "assigned", "a")
	must(t, st.update(func(s *State) error {
		task := s.Tasks["T1"]
		task.Owner = "c"
		task.Participants = append(task.Participants, "c")
		s.expireReports()
		return nil
	}))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == reviewer.ID && m.State != DeliveryStatePending {
			t.Fatalf("reviewer lost still-current assignment: %+v", m)
		}
		if m.ID == owner.ID && m.State != DeliveryStateSuperseded {
			t.Fatalf("former owner kept stale personal assignment: %+v", m)
		}
	}
}

func TestLegacyDispatchNoticesGainFreshnessReferences(t *testing.T) {
	s := &State{
		Version: 3,
		Tasks: map[string]*Task{
			"T1": {ID: "T1", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a", "b"}},
			"T2": {ID: "T2", State: TaskPhaseReady, Dispatch: DispatchModeOpen},
		},
		Messages: []*Message{
			{ID: "M1", From: "master", To: "b", Task: "T1", Text: assignedNotice(&Task{ID: "T1"}), State: DeliveryStatePending},
			{ID: "M2", From: "master", To: "b", Task: "T2", Text: availableNotice(&Task{ID: "T2", Title: "Open"}), State: DeliveryStateSending},
			{ID: "M3", From: "master", To: "c", Task: "T1", RequestKey: ccKeyPrefix + "T1:a:c", Text: "FYI", State: DeliveryStatePending},
			{ID: "M4", From: "a", To: "b", Task: "T1", Text: "please review", State: DeliveryStatePending},
			{ID: "M5", From: "master", To: "a", Task: "T1", Text: assignedNotice(&Task{ID: "T1"}), State: DeliveryStateSent},
		},
	}
	normalizeState(s)
	if s.Version != stateVersion {
		t.Fatalf("version = %d", s.Version)
	}
	for i, want := range []ReportReference{{Kind: "assigned", Owner: "a"}, {Kind: "available"}, {Kind: "cc", Owner: "a"}} {
		if got := s.Messages[i].Report; got == nil || got.Kind != want.Kind || got.Owner != want.Owner {
			t.Fatalf("legacy notice %d not tagged: %+v", i, got)
		}
	}
	if s.Messages[3].Report != nil || s.Messages[4].Report != nil {
		t.Fatal("migration changed personal or already-sent history")
	}
}
