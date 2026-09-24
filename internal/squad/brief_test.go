package squad

import (
	"encoding/json"
	"testing"
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

func ledgerJSON(t *testing.T, st *Store) string {
	t.Helper()
	var raw string
	must(t, st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw))
	return raw
}

// A Brief request recorded before the report was removed still loads and is
// never delivered, retried or rewritten.
func TestLegacyBriefIsAuditOnly(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	var id string
	must(t, st.update(func(s *State) error {
		m := s.message(UserSender, "master", "T1", "old Brief", "")
		m.RequestKey = UserSender + ":brief:T1"
		id = m.ID
		return nil
	}))
	before := ledgerJSON(t, st)
	must(t, st.syncMessages())
	must(t, st.deliver(id))
	if err := messageCommand(st, "master", []string{"message", "retry", id}, options{}); err == nil {
		t.Fatal("legacy retry accepted")
	}
	if before != ledgerJSON(t, st) {
		t.Fatal("legacy history changed")
	}
	s, e := st.read()
	must(t, e)
	s.Messages[0].recoverDelivery()
	if s.Messages[0].Attempts != 0 || s.Messages[0].State != DeliveryStatePending {
		t.Fatal("legacy recovery changed history")
	}
}
