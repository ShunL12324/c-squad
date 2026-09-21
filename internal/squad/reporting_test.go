package squad

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

func TestFourMemberReportingAggregatesCurrentEvidence(t *testing.T) {
	st := submissionStore(t)
	must(t, st.update(func(s *State) error {
		s.Members["c"] = &Member{ID: "c", Engine: config.Claude, State: MemberStateIdle, Generation: 1}
		s.Tasks["T1"].Participants = []string{"a", "b", "c"}
		s.Tasks["T1"].Milestones = []Milestone{{Name: "inspection", State: MilestoneStatePending}, {Name: "cross_review", State: MilestoneStatePending}}
		return nil
	}))
	must(t, taskCommand(st, "a", []string{"progress", "T1"}, options{"text": "inspection done"}))
	for _, ms := range []string{"inspection", "cross_review"} {
		must(t, taskCommand(st, "a", []string{"milestone", "T1"}, options{"name": ms}))
	}
	s, err := st.read()
	must(t, err)
	if len(s.Messages) != 0 {
		t.Fatal("ordinary progress woke master")
	}
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "cross review delivered"}))
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "cross review delivered"}))
	sub := currentSubmission(t, st).Submission
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "true", "summary": "independent review"}))
	s, err = st.read()
	must(t, err)
	if len(s.Messages) != 1 {
		t.Fatal("submission retry or individual passing evidence notified master")
	}
	must(t, taskCommand(st, "c", []string{"evidence", "T1"}, options{"submission": sub, "kind": "test", "passed": "true", "summary": "cross checks passed"}))
	s, err = st.read()
	must(t, err)
	if len(s.Messages) != 2 || s.Messages[0].State != DeliveryStateSuperseded || s.Messages[1].Report.Kind != "ready" {
		t.Fatalf("queued delivery not folded into ready report: %+v", s.Messages)
	}
	report := s.reportText(s.Messages[1])
	for _, want := range []string{sub, "independent review", "cross checks passed", `"member":"b"`, `"member":"c"`} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %s: %s", want, report)
		}
	}
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "true", "summary": "confirmation"}))
	s, err = st.read()
	must(t, err)
	if len(s.Messages) != 2 {
		t.Fatal("repeated passing evidence created another ready report")
	}
	// A failed correction escalates even after readiness, then its resolution
	// invalidates the delayed failure and produces a fresh consolidated report.
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "false", "summary": "counterexample"}))
	s, err = st.read()
	must(t, err)
	if s.Messages[1].State != DeliveryStateSuperseded || s.Messages[2].Report.Kind != "failure" {
		t.Fatal("failure not escalated")
	}
	must(t, taskCommand(st, "b", []string{"evidence", "T1"}, options{"submission": sub, "kind": "review", "passed": "true", "summary": "counterexample resolved"}))
	s, err = st.read()
	must(t, err)
	if s.Messages[2].State != DeliveryStateSuperseded || s.Messages[3].Report.Kind != "ready" {
		t.Fatal("correction did not replace stale failure")
	}
	must(t, taskCommand(st, "master", []string{"reopen", "T1"}, options{}))
	s, err = st.read()
	must(t, err)
	if s.Messages[3].State != DeliveryStateSuperseded {
		t.Fatal("withdrawn candidate report remains active")
	}
}

func TestReportingDecisionsExpireWhenResolved(t *testing.T) {
	st := submissionStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"].Milestones = []Milestone{{Name: "plan", Gate: true, State: MilestoneStatePending}}
		return nil
	}))
	must(t, taskCommand(st, "a", []string{"milestone", "T1"}, options{"name": "plan"}))
	s, err := st.read()
	must(t, err)
	gate := s.Messages[0].ID
	if s.Messages[0].Report.Kind != "decision" {
		t.Fatal("gate was silenced")
	}
	if !strings.Contains(s.reportText(s.Messages[0]), `"milestone":"plan"`) {
		t.Fatal("gate report omitted the required decision")
	}
	must(t, taskCommand(st, "master", []string{"gate", "T1"}, options{"name": "plan"}))
	must(t, helpCommand(st, "a", []string{"request"}, options{"task": "T1", "text": "permission needed"}))
	s, err = st.read()
	must(t, err)
	var qid, mid string
	for id := range s.Questions {
		qid = id
	}
	for _, m := range s.Messages {
		if m.Report != nil && m.Report.Question == qid {
			mid = m.ID
		}
	}
	if mid == "" {
		t.Fatal("blocking question swallowed")
	}
	must(t, helpCommand(st, "master", []string{"answer", qid}, options{"text": "resolved"}))
	for _, id := range []string{gate, mid} {
		must(t, st.deliver(id))
	}
	s, err = st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == gate || m.ID == mid {
			if m.State != DeliveryStateSuperseded || m.Attempts != 0 {
				t.Fatalf("stale decision delivered: %+v", m)
			}
			m.resetDelivery()
			if m.State != DeliveryStateSuperseded {
				t.Fatal("restart resurrected stale report")
			}
		}
	}
}

func TestReportingTransportNoReinjectionAndAckBeforeDelivery(t *testing.T) {
	st := testStore(t)
	sock := filepath.Join(t.TempDir(), "master.sock")
	listener, err := net.Listen("unix", sock)
	must(t, err)
	defer listener.Close()
	received := make(chan string, 4)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			var frame map[string]any
			_ = json.NewDecoder(c).Decode(&frame)
			_ = c.Close()
			raw, _ := json.Marshal(frame)
			received <- string(raw)
		}
	}()
	must(t, st.update(func(s *State) error {
		s.Members["master"].Peer = sock
		s.Members["master"].EngineID = "test"
		return nil
	}))
	opts := options{"text": "urgent failure", "request-id": "one-failure"}
	must(t, messageCommand(st, "a", []string{"message", "send", "master"}, opts))
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("urgent message not delivered")
	}
	must(t, st.update(func(s *State) error {
		s.Messages[0].Attempt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
		return nil
	}))
	must(t, st.syncMessages())
	must(t, messageCommand(st, "a", []string{"message", "send", "master"}, opts))
	s, err := st.read()
	must(t, err)
	if len(s.Messages) != 1 || s.Messages[0].Attempts != 1 || s.Messages[0].State != DeliveryStateNeedsAttention {
		t.Fatalf("successful transport retried or lost observability: %+v", s.Messages)
	}
	// Manual retry is still an explicit escape hatch, even after needs_attention.
	must(t, messageCommand(st, "master", []string{"message", "retry", s.Messages[0].ID}, options{}))
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("explicit retry not delivered")
	}

	must(t, messageCommand(st, "master", []string{"message", "ack", s.Messages[0].ID}, options{}))
	must(t, st.syncMessages())
	var early string
	must(t, st.update(func(s *State) error {
		early = s.message("a", "master", "", "already read via inbox", "").ID
		return nil
	}))
	must(t, messageCommand(st, "master", []string{"message", "ack", early}, options{}))
	must(t, st.deliver(early))
	s, err = st.read()
	must(t, err)
	if s.Messages[1].Attempts != 0 || s.Messages[1].State != DeliveryStateAcknowledged {
		t.Fatal("ACK before transport ignored")
	}
	select {
	case msg := <-received:
		t.Fatalf("duplicate/acked message injected: %s", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReportRefreshesFactsAtTransport(t *testing.T) {
	st := submissionStore(t)
	must(t, taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "delivered result"}))
	must(t, taskCommand(st, "a", []string{"progress", "T1"}, options{"text": "latest validation boundary"}))
	sock := filepath.Join(t.TempDir(), "inbox.sock")
	l, err := net.Listen("unix", sock)
	must(t, err)
	defer l.Close()
	received := make(chan string, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var frame map[string]any
		_ = json.NewDecoder(c).Decode(&frame)
		raw, _ := json.Marshal(frame)
		received <- string(raw)
	}()
	must(t, st.update(func(s *State) error {
		s.Members["master"].Peer = sock
		s.Members["master"].EngineID = "test"
		return nil
	}))
	must(t, st.syncMessages())
	select {
	case msg := <-received:
		if !strings.Contains(msg, "latest validation boundary") {
			t.Fatal("delivered old snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("report not delivered")
	}
	s, err := st.read()
	must(t, err)
	if !strings.Contains(s.Messages[0].Text, "latest validation boundary") {
		t.Fatal("message history does not retain the delivered snapshot")
	}
}

func TestReportSummaryOmitsInstructionsAndHistoricalEvidence(t *testing.T) {
	st := submissionStore(t)
	must(t, st.update(func(s *State) error {
		task := s.Tasks["T1"]
		task.Title = strings.Repeat("标题", 1000)
		task.Description = strings.Repeat("SECRET_DESCRIPTION", 10000)
		task.Acceptance = "SECRET_ACCEPTANCE"
		task.Setup = "SECRET_SETUP"
		task.Workspace = "/SECRET_WORKSPACE"
		task.Submission, task.Candidate = "T1-r2", "abc123"
		task.SubmissionSummary = strings.Repeat("结论", 1000)
		task.Progress = strings.Repeat("进展", 1000)
		task.Evidence = []Evidence{
			{Member: "b", Kind: EvidenceReview, Submission: "T1-r1", SHA: "old", Summary: "OLD_SUBMISSION"},
			{Member: "b", Kind: EvidenceReview, Submission: "T1-r2", SHA: "abc123", Summary: strings.Repeat("OLD_RESULT", 10000)},
			{Member: "b", Kind: EvidenceReview, Submission: "T1-r2", SHA: "abc123", Passed: true, Summary: strings.Repeat("最新结论", 1000)},
			{Member: "c", Kind: EvidenceTest, Submission: "T1-r2", SHA: "abc123", Passed: true, Summary: "current test"},
		}
		return nil
	}))
	s, err := st.read()
	must(t, err)
	text := s.reportText(&Message{Task: "T1", Report: &ReportReference{Kind: "ready", Submission: "T1-r2"}})
	for _, forbidden := range []string{"SECRET_", "OLD_SUBMISSION", "OLD_RESULT", `"description"`, `"acceptance"`, `"setup"`, `"workspace"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default report leaked %s", forbidden)
		}
	}
	if len(text) > 7000 || !strings.Contains(text, "[truncated; task inspect for full text]") {
		t.Fatalf("summary length/truncation: %d bytes", len(text))
	}
	_, body, _ := strings.Cut(text, "\n")
	var report compactTaskReport
	must(t, json.Unmarshal([]byte(body), &report))
	if len(report.Evidence) != 2 || report.EvidenceHistoryOmitted != 2 || report.FailingEvidence != 0 {
		t.Fatalf("not current evidence only: %+v", report)
	}
	for _, e := range report.Evidence {
		if e.Index < 3 || e.Submission != "T1-r2" || e.SHA != "abc123" || !e.Passed {
			t.Fatalf("lost exact evidence reference: %+v", e)
		}
	}
	if len(s.Tasks["T1"].Evidence[2].Summary) < 1000 {
		t.Fatal("report mutated original evidence")
	}
}

func TestReportSummaryBoundsListsAndKeepsTriggeringFailure(t *testing.T) {
	s := &State{Tasks: map[string]*Task{}, Questions: map[string]*Question{}}
	task := &Task{ID: "T1", Submission: "T1-r1"}
	s.Tasks[task.ID] = task
	for i := 0; i < 100; i++ {
		member := fmt.Sprintf("member%d", i)
		task.Evidence = append(task.Evidence, Evidence{Member: member, Kind: EvidenceReview, Submission: task.Submission, Summary: strings.Repeat("failure", 1000)})
		task.Blockers = append(task.Blockers, strings.Repeat("blocker", 1000))
		id := fmt.Sprintf("Q%d", i)
		s.Questions[id] = &Question{ID: id, Task: task.ID, State: QuestionStateOpen, Text: strings.Repeat("QUESTION_DETAIL", 1000)}
	}
	msg := &Message{Task: task.ID, Report: &ReportReference{Kind: "failure", Submission: task.Submission, Evidence: 1}}
	text := s.reportText(msg)
	_, body, _ := strings.Cut(text, "\n")
	var report compactTaskReport
	must(t, json.Unmarshal([]byte(body), &report))
	if len(report.Evidence) != 12 || report.EvidenceOmitted != 88 || report.FailingEvidence != 100 || report.Evidence[0].Index != 1 {
		t.Fatalf("failure lost or lists unbounded: %+v", report)
	}
	if len(report.OpenQuestions) != 8 || report.QuestionsOmitted != 92 || len(report.Blockers) != 8 || report.BlockersOmitted != 92 {
		t.Fatal("unbounded blockers/questions")
	}
	if len(text) > 10000 || strings.Contains(text, "QUESTION_DETAIL") {
		t.Fatal("report is not compact")
	}
	if text != s.reportText(msg) {
		t.Fatal("report ordering is unstable")
	}
}
