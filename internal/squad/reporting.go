package squad

import (
	"encoding/json"
	"fmt"
)

// ReportReference ties automatic notifications to ledger facts. Free-form
// messages intentionally have no reference: their urgency cannot be inferred.
type ReportReference struct {
	Kind       string `json:"kind"`
	Submission string `json:"submission,omitempty"`
	Milestone  string `json:"milestone,omitempty"`
	Question   string `json:"question,omitempty"`
	Evidence   int    `json:"evidence,omitempty"` // One-based index in this submission.
}

func evidenceReady(t *Task) bool {
	if t.Submission == "" || checkEvidence(t) != nil {
		return false
	}
	// Non-code work need not have evidence to be approved, but an evidence
	// notification only represents a complete review/test pair.
	seen := map[EvidenceKind]bool{}
	for _, e := range t.Evidence {
		if e.Submission == t.Submission && e.SHA == t.Candidate && e.Passed {
			seen[e.Kind] = true
		}
	}
	return seen[EvidenceReview] && seen[EvidenceTest]
}

func (s *State) reportCurrent(m *Message) bool {
	r := m.Report
	if r == nil {
		return true
	}
	if r.Question != "" {
		q := s.Questions[r.Question]
		return q != nil && q.State == QuestionStateOpen
	}
	t := s.Tasks[m.Task]
	if t == nil {
		return false
	}
	if r.Milestone != "" {
		for _, ms := range t.Milestones {
			if ms.Name == r.Milestone {
				return ms.State == MilestoneStateAwaitingApproval
			}
		}
		return false
	}
	if t.Submission != r.Submission || t.State != TaskPhaseInReview {
		return false
	}
	switch r.Kind {
	case "ready":
		return evidenceReady(t)
	case "failure":
		if r.Evidence < 1 || r.Evidence > len(t.Evidence) {
			return false
		}
		e := t.Evidence[r.Evidence-1]
		for _, later := range t.Evidence[r.Evidence:] {
			if later.Member == e.Member && later.Kind == e.Kind {
				return false
			}
		}
		return !e.Passed
	case "delivery":
		return true
	}
	return true
}

func (s *State) expireReports() {
	for _, m := range s.Messages {
		if m.State != DeliveryStateAcknowledged && m.State != DeliveryStateSuperseded && !s.reportCurrent(m) {
			m.State = DeliveryStateSuperseded
			m.Error = "superseded by current ledger state; retained for history"
		}
	}
}

func (s *State) taskReport(actor string, t *Task, ref *ReportReference) {
	// Repeating an immutable submission is idempotent. A later readiness
	// transition (after a correction) remains a new report.
	if ref.Kind == "delivery" {
		for _, m := range s.Messages {
			if m.Task == t.ID && m.Report != nil && m.Report.Kind == "delivery" && m.Report.Submission == ref.Submission {
				return
			}
		}
	}
	// Fold a not-yet-delivered submission into its newer evidence summary.
	if ref.Kind == "ready" || ref.Kind == "failure" {
		for _, m := range s.Messages {
			if m.Task == t.ID && m.Report != nil && m.Report.Kind == "delivery" && m.Report.Submission == ref.Submission && m.State == DeliveryStatePending {
				m.State = DeliveryStateSuperseded
			}
		}
	}
	m := s.message(actor, "master", t.ID, "", "")
	m.Report = ref
	m.Text = s.reportText(m)
}

func (s *State) reportText(m *Message) string {
	if m.Report == nil || m.Report.Question != "" {
		return m.Text
	}
	t := s.Tasks[m.Task]
	if t == nil {
		return m.Text
	}
	// Include full evidence with member, immutable submission, SHA and outcome;
	// no inferred success or automatic approval is introduced by the report.
	snapshot := struct {
		Task          *Task       `json:"task"`
		OpenQuestions []*Question `json:"open_questions"`
	}{Task: t, OpenQuestions: []*Question{}}
	for _, q := range s.Questions {
		if q.Task == t.ID && q.State == QuestionStateOpen {
			snapshot.OpenQuestions = append(snapshot.OpenQuestions, q)
		}
	}
	body, _ := json.Marshal(snapshot)
	return fmt.Sprintf("Task report (%s). Read current ledger before acting; task inspect %s expands the record.\n%s", m.Report.Kind, t.ID, body)
}
