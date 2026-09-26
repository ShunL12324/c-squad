package squad

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func pendingRoutineReports(t *testing.T, st *Store, count int) []string {
	t.Helper()
	var ids []string
	must(t, st.update(func(s *State) error {
		s.Members["master"].State = MemberStateIdle
		for i := 0; i < count; i++ {
			id := "T" + strconv.Itoa(i+1)
			s.Tasks[id] = &Task{ID: id, State: TaskPhaseInReview, Submission: id + "-r1", Owner: "a", SubmissionSummary: "Ready", Progress: "Ready"}
			m := s.message("a", "master", id, "", "")
			m.Report = &ReportReference{Kind: "delivery", Submission: id + "-r1"}
			ids = append(ids, m.ID)
		}
		return nil
	}))
	return ids
}

func TestRoutineBatchPreservesMessagesAndSkipsStale(t *testing.T) {
	st, log := busyCodexStore(t)
	ids := pendingRoutineReports(t, st, 3)
	must(t, st.update(func(s *State) error { s.Tasks["T2"].State = TaskPhaseDone; return nil }))
	for _, id := range ids {
		must(t, st.deliver(id))
	}
	got := queueCalls(t, log)
	if strings.Count(got, "queue --thread") != 1 || !strings.Contains(got, "message_id="+ids[0]) || !strings.Contains(got, "message_id="+ids[2]) || strings.Contains(got, "message_id="+ids[1]) {
		t.Fatalf("wrong batch: %s", got)
	}
	s, err := st.read()
	must(t, err)
	for i, m := range s.Messages {
		if i == 1 {
			if m.State != DeliveryStateSuperseded || m.Attempts != 0 {
				t.Fatalf("stale message transported: %+v", m)
			}
		} else if m.State != DeliveryStateSent || m.Attempts != 1 || !strings.Contains(m.Text, m.Task) {
			t.Fatalf("individual transport record lost: %+v", m)
		}
	}
	if strings.Contains(got, `"progress":"Ready"`) {
		t.Fatal("duplicate conclusion/progress still in report")
	}
}

func TestRoutineBatchStopsAtFreeformBoundary(t *testing.T) {
	st, log := busyCodexStore(t)
	ids := pendingRoutineReports(t, st, 3)
	must(t, st.update(func(s *State) error {
		s.Messages[1].Report = nil
		s.Messages[1].Text = "Please act now"
		return nil
	}))
	must(t, st.deliver(ids[0]))
	if strings.Contains(queueCalls(t, log), ids[1]) || strings.Contains(queueCalls(t, log), ids[2]) {
		t.Fatal("batch crossed freeform boundary")
	}
	must(t, st.deliver(ids[1]))
	must(t, st.deliver(ids[2]))
	if strings.Count(queueCalls(t, log), "queue --thread") != 3 {
		t.Fatal("independent messages were lost or combined")
	}
}

func TestRoutineBatchFailureRestoresEveryClaim(t *testing.T) {
	st, _ := busyCodexStore(t)
	ids := pendingRoutineReports(t, st, 2)
	s, err := st.read()
	must(t, err)
	must(t, os.WriteFile(filepath.Join(s.Members["master"].Cwd, "codex-helper"), []byte("#!/bin/sh\nexit 1\n"), 0700))
	if err := st.deliver(ids[0]); err == nil {
		t.Fatal("transport failure hidden")
	}
	s, err = st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.State != DeliveryStatePending || m.Attempts != 1 || m.Error == "" {
			t.Fatalf("failed batch lost retry state: %+v", m)
		}
	}
}

func TestRoutineBatchBoundaries(t *testing.T) {
	for _, boundary := range []string{"sending", "bootstrap", "recipient", "urgent", "backoff"} {
		t.Run(boundary, func(t *testing.T) {
			st, log := busyCodexStore(t)
			ids := pendingRoutineReports(t, st, 2)
			must(t, st.update(func(s *State) error {
				m := s.Messages[1]
				switch boundary {
				case "sending":
					m.State, m.Attempt = DeliveryStateSending, now()
				case "bootstrap":
					m.BootstrapGeneration = s.Members["master"].Generation
				case "recipient":
					m.To = "a"
				case "urgent":
					m.Report.Kind = "decision"
				case "backoff":
					m.Attempt, m.Attempts = now(), 1
				}
				return nil
			}))
			must(t, st.deliver(ids[0]))
			if strings.Contains(queueCalls(t, log), "message_id="+ids[1]) {
				t.Fatalf("batch crossed %s boundary", boundary)
			}
		})
	}
}

func TestRoutineBatchCountBound(t *testing.T) {
	st, log := busyCodexStore(t)
	ids := pendingRoutineReports(t, st, routineBatchCount+1)
	must(t, st.deliver(ids[0]))
	got := queueCalls(t, log)
	if strings.Count(got, "message_id=") != routineBatchCount || strings.Contains(got, "message_id="+ids[routineBatchCount]) {
		t.Fatalf("unbounded batch: %s", got)
	}
}

func TestRoutineBatchByteBound(t *testing.T) {
	st, log := busyCodexStore(t)
	ids := pendingRoutineReports(t, st, 2)
	must(t, st.update(func(s *State) error {
		s.Tasks["T2"].Participants = []string{"master"}
		s.Messages[1].Report = &ReportReference{Kind: "assigned", Owner: "a"}
		s.Messages[1].Text = strings.Repeat("x", routineBatchBytes)
		return nil
	}))
	must(t, st.deliver(ids[0]))
	if strings.Contains(queueCalls(t, log), "message_id="+ids[1]) {
		t.Fatal("oversize notice appended to batch")
	}
	s, err := st.read()
	must(t, err)
	if s.Messages[1].Attempts != 0 || s.Messages[1].State != DeliveryStatePending {
		t.Fatal("untransported oversize notice consumed a claim")
	}
}
