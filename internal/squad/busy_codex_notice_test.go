package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

// A local helper records native queue calls without starting a Codex session.
func busyCodexStore(t *testing.T) (*Store, string) {
	t.Helper()
	st := testStore(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "queued")
	command := filepath.Join(dir, "codex-helper")
	must(t, os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$QUEUE_LOG\"\n"), 0700))
	cfg := config.Defaults()
	cfg.EngineCommands = map[config.Engine]config.Command{config.Codex: {Executable: command}}
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		m := s.Members["master"]
		m.Engine, m.EngineID, m.Cwd = config.Codex, "thread-1", dir
		m.Env = map[string]string{"QUEUE_LOG": log}
		m.State = MemberStateWorking
		m.LastSeen = now()
		s.Tasks["T1"] = &Task{ID: "T1", Title: "work", State: TaskPhaseInReview, Owner: "a", Participants: []string{"a"}, Submission: "T1-r1"}
		return nil
	}))
	return st, log
}

func queueCalls(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	must(t, err)
	return string(b)
}

func TestBusyCodexNoticeKindsAreExplicit(t *testing.T) {
	member := &Member{Engine: config.Codex, State: MemberStateWorking, LastSeen: now()}
	for _, kind := range []string{"delivery", "ready", "available", "assigned", "cc"} {
		if !deferBusyCodexNotice(member, &Message{Report: &ReportReference{Kind: kind}}) {
			t.Fatalf("routine %s report was not held", kind)
		}
	}
	for _, kind := range []string{"failure", "decision", "recovery", "stall"} {
		if deferBusyCodexNotice(member, &Message{Report: &ReportReference{Kind: kind}}) {
			t.Fatalf("urgent %s report was held", kind)
		}
	}
	if deferBusyCodexNotice(member, &Message{Text: "human message"}) {
		t.Fatal("freeform message was held")
	}
	member.State = MemberStateIdle
	if deferBusyCodexNotice(member, &Message{Report: &ReportReference{Kind: "delivery"}}) {
		t.Fatal("idle recipient was held")
	}
	member.Engine, member.State = config.Claude, MemberStateWorking
	if deferBusyCodexNotice(member, &Message{Report: &ReportReference{Kind: "delivery"}}) {
		t.Fatal("Claude transport was changed")
	}
	member.Engine = config.Codex
	member.LastSeen = time.Now().Add(-6 * time.Minute).Format(time.RFC3339Nano)
	if deferBusyCodexNotice(member, &Message{Report: &ReportReference{Kind: "delivery"}}) {
		t.Fatal("stale working hook held a notice indefinitely")
	}
}

func TestBusyCodexStaleAutomaticNoticeNeverReachesNativeQueue(t *testing.T) {
	st, log := busyCodexStore(t)
	var id string
	must(t, st.update(func(s *State) error {
		m := s.message("a", "master", "T1", "", "")
		m.Report = &ReportReference{Kind: "delivery", Submission: "T1-r1"}
		id = m.ID
		return nil
	}))
	must(t, st.deliver(id))
	s, err := st.read()
	must(t, err)
	if m := s.Messages[0]; m.State != DeliveryStatePending || m.Attempts != 0 || queueCalls(t, log) != "" {
		t.Fatalf("busy recipient received automatic report: %+v", m)
	}
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].State = TaskPhaseDone
		s.Members["master"].State = MemberStateIdle
		return nil
	}))
	must(t, st.deliver(id))
	s, err = st.read()
	must(t, err)
	if m := s.Messages[0]; m.State != DeliveryStateSuperseded || m.Attempts != 0 || queueCalls(t, log) != "" {
		t.Fatalf("obsolete report reached native queue: %+v", m)
	}
}

func TestBusyCodexCurrentAutomaticNoticeDeliversAtIdle(t *testing.T) {
	st, log := busyCodexStore(t)
	var id string
	must(t, st.update(func(s *State) error {
		m := s.message("a", "master", "T1", "", "")
		m.Report = &ReportReference{Kind: "delivery", Submission: "T1-r1"}
		id = m.ID
		return nil
	}))
	must(t, st.deliver(id))
	if queueCalls(t, log) != "" {
		t.Fatal("automatic report queued during busy turn")
	}
	must(t, st.update(func(s *State) error { s.Members["master"].State = MemberStateIdle; return nil }))
	must(t, st.deliver(id))
	s, err := st.read()
	must(t, err)
	if m := s.Messages[0]; m.State != DeliveryStateSent || m.Attempts != 1 {
		t.Fatalf("current report not sent at idle: %+v", m)
	}
	if calls := queueCalls(t, log); !strings.Contains(calls, "queue --thread thread-1 --message") {
		t.Fatalf("current report did not reach native queue: %s", calls)
	}
}

func TestBusyCodexFreeformAndUrgentReportsDeliverImmediately(t *testing.T) {
	st, log := busyCodexStore(t)
	var ids []string
	must(t, st.update(func(s *State) error {
		ids = append(ids, s.message("a", "master", "T1", "Please review this now", "").ID)
		s.Tasks["T1"].Evidence = []Evidence{{Member: "b", Kind: EvidenceReview, Submission: "T1-r1", Passed: false}}
		failure := s.message("a", "master", "T1", "", "")
		failure.Report = &ReportReference{Kind: "failure", Submission: "T1-r1", Evidence: 1}
		ids = append(ids, failure.ID)
		return nil
	}))
	for _, id := range ids {
		must(t, st.deliver(id))
	}
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.State != DeliveryStateSent || m.Attempts != 1 {
			t.Fatalf("urgent notice was delayed: %+v", m)
		}
	}
	if calls := queueCalls(t, log); strings.Count(calls, "queue --thread thread-1 --message") != 2 {
		t.Fatalf("expected freeform and failure in native queue: %s", calls)
	}
}
