package squad

import (
	"encoding/json"
	"testing"
)

func TestUserConfirmationPreservesTechnicalDelivery(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	before, _ := st.read()
	must(t, canOwn(before, &Task{ID: "T2"}, "a"))
	_, err := st.confirmTask("T1")
	must(t, err)
	after, _ := st.read()
	c := after.Tasks["T1"].UserConfirmation
	if c == nil || c.Actor != UserSender || c.At == "" || c.Phase != TaskPhaseDone {
		t.Fatalf("missing audit: %+v", c)
	}
	first := taskJSON(t, st, "T1")
	_, err = st.confirmTask("T1")
	must(t, err)
	if first != taskJSON(t, st, "T1") {
		t.Fatal("repeat confirmation changed record")
	}
	after.Tasks["T1"].UserConfirmation = nil
	a, _ := json.Marshal(before.Tasks["T1"])
	b, _ := json.Marshal(after.Tasks["T1"])
	if string(a) != string(b) {
		t.Fatalf("confirmation changed technical state: %s / %s", a, b)
	}
	snap, err := st.panelSnapshot()
	must(t, err)
	if snap.Tasks[0].CanConfirm {
		t.Fatal("confirmed task still offers confirmation")
	}
}

func TestConfirmationRejectsUnfinishedTasksAndAgents(t *testing.T) {
	st := briefStore(t, TaskPhaseInReview)
	before := taskJSON(t, st, "T1")
	if _, err := st.confirmTask("T1"); err == nil {
		t.Fatal("unfinished delivery accepted")
	}
	if before != taskJSON(t, st, "T1") {
		t.Fatal("failed confirmation mutated task")
	}
	must(t, st.update(func(s *State) error { s.Tasks["T1"].State = TaskPhaseDone; return nil }))
	snap, err := st.panelSnapshot()
	must(t, err)
	if !snap.Tasks[0].CanConfirm {
		t.Fatal("delivery missing confirmation action")
	}
	if err := taskCommand(st, "a", []string{"confirm", "T1"}, options{}); err == nil {
		t.Fatal("worker confirmed for user")
	}
	st.Generation = 1
	if err := taskCommand(st, "master", []string{"confirm", "T1"}, options{}); err == nil {
		t.Fatal("master agent confirmed for user")
	}
	st.Generation = 0
	must(t, taskCommand(st, "master", []string{"confirm", "T1"}, options{}))
}

func TestConfirmThroughExecuteAfterTeamStopped(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	must(t, st.update(func(s *State) error { s.Active = false; return nil }))
	for _, key := range []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION"} {
		t.Setenv(key, "")
	}
	must(t, Execute([]string{"task", "confirm", "T1"}, options{"team": st.Dir}, nil))
	s, err := st.read()
	must(t, err)
	if s.Active || s.Tasks["T1"].UserConfirmation == nil {
		t.Fatal("stopped confirmation failed or restarted team")
	}
	bindSession(t, st, "master", "1")
	if err := Execute([]string{"task", "confirm", "T1"}, options{}, nil); err == nil {
		t.Fatal("agent accepted user confirmation")
	}
}
