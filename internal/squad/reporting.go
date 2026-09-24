package squad

import (
	"encoding/json"
	"fmt"
	"sort"
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
	// A recovery notification names no submission, so it answers before the
	// review checks below. It stays current only while that task still needs
	// this recipient to act.
	if r.Kind == "recovery" {
		return t.State != TaskPhaseDone && m.To == s.recoveryRecipient(t)
	}
	if r.Kind == "stall" {
		return s.stallCurrent(m)
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

const recoveryNoticeText = "Task recovered after team interruption. Inspect board, handoff and actual workspace before continuing. Preserve review/approval gates; do not repeat completed changes or merge on your own. Report current status to master."

// recoveryRecipient routes a task recovery notification to its owner, or to
// master when that owner is gone.
func (s *State) recoveryRecipient(t *Task) string {
	to := t.Owner
	if m := s.Members[to]; m == nil || m.State == MemberStateRemoved {
		to = "master"
	}
	return to
}

// recoveryNotice re-engages the member a task is waiting on. A stable
// RequestKey reuses a notification that recipient has not received yet, so
// repeated resume attempts cannot accumulate identical wake-ups. A delivered
// notification is never reused: a later interruption is genuinely new.
func (s *State) recoveryNotice(t *Task) {
	to := s.recoveryRecipient(t)
	key := "master:recovery:" + t.ID
	for _, old := range s.Messages {
		if old.RequestKey == key && old.To == to && (old.State == DeliveryStatePending || old.State == DeliveryStateSending) {
			return
		}
	}
	m := s.message("master", to, t.ID, recoveryNoticeText, "")
	m.RequestKey = key
	m.Report = &ReportReference{Kind: "recovery"}
}

// queueRecoveryNotices re-engages every task an interruption left open.
func (s *State) queueRecoveryNotices() {
	for _, t := range s.Tasks {
		if t.State != TaskPhaseDone {
			s.recoveryNotice(t)
		}
	}
}

func (s *State) reportText(m *Message) string {
	// A recovery notification carries behaviour the ledger snapshot cannot
	// express (preserve gates, do not repeat completed work), so keep its text.
	if m.Report == nil || m.Report.Question != "" || m.Report.Kind == "recovery" || m.Report.Kind == "stall" {
		return m.Text
	}
	t := s.Tasks[m.Task]
	if t == nil {
		return m.Text
	}

	// Only current facts enter the default notification. Full task instructions,
	// paths and evidence history remain available via task inspect.
	snapshot := compactTaskReport{
		Trigger: *m.Report,
		ID:      t.ID, Title: reportExcerpt(t.Title, 120), State: t.State,
		Owner: t.Owner, Submission: t.Submission, Candidate: t.Candidate,
		Conclusion: reportExcerpt(t.SubmissionSummary, 480),
		Progress:   reportExcerpt(t.Progress, 480),
		Evidence:   []reportEvidence{}, OpenQuestions: []string{}, Blockers: []string{},
	}
	snapshot.Trigger.Milestone = reportExcerpt(snapshot.Trigger.Milestone, 160)
	for _, b := range t.Blockers {
		if len(snapshot.Blockers) == 8 {
			break
		}
		snapshot.Blockers = append(snapshot.Blockers, reportExcerpt(b, 160))
	}
	snapshot.BlockersOmitted = len(t.Blockers) - len(snapshot.Blockers)
	for _, q := range s.Questions {
		if q.Task == t.ID && q.State == QuestionStateOpen {
			snapshot.OpenQuestions = append(snapshot.OpenQuestions, q.ID)
		}
	}
	sort.Strings(snapshot.OpenQuestions)
	if len(snapshot.OpenQuestions) > 8 {
		snapshot.QuestionsOmitted = len(snapshot.OpenQuestions) - 8
		snapshot.OpenQuestions = snapshot.OpenQuestions[:8]
	}
	latest := map[[2]string]int{}
	for i, e := range t.Evidence {
		if e.Submission == t.Submission && e.SHA == t.Candidate {
			latest[[2]string{e.Member, string(e.Kind)}] = i
		}
	}
	indices := make([]int, 0, len(latest))
	for _, i := range latest {
		indices = append(indices, i)
		if !t.Evidence[i].Passed {
			snapshot.FailingEvidence++
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		// A failure that triggered this report must survive truncation.
		if indices[i] == indices[j] {
			return false
		}
		if indices[i]+1 == m.Report.Evidence {
			return true
		}
		if indices[j]+1 == m.Report.Evidence {
			return false
		}
		a, b := t.Evidence[indices[i]], t.Evidence[indices[j]]
		if a.Passed != b.Passed {
			return !a.Passed
		}
		return indices[i] > indices[j]
	})
	snapshot.EvidenceHistoryOmitted = len(t.Evidence) - len(indices)
	for _, i := range indices {
		if len(snapshot.Evidence) == 12 {
			break
		}
		e := t.Evidence[i]
		e.Summary = reportExcerpt(e.Summary, 160)
		snapshot.Evidence = append(snapshot.Evidence, reportEvidence{Index: i + 1, Evidence: e})
	}
	snapshot.EvidenceOmitted = len(indices) - len(snapshot.Evidence)
	body, _ := json.Marshal(snapshot)
	return fmt.Sprintf("Task report (%s). Read current ledger before acting; task inspect %s expands the record (evidence indices are one-based).\n%s", m.Report.Kind, t.ID, body)
}

type reportEvidence struct {
	Index int `json:"index"`
	Evidence
}

type compactTaskReport struct {
	Trigger                ReportReference  `json:"trigger"`
	ID                     string           `json:"id"`
	Title                  string           `json:"title"`
	State                  TaskPhase        `json:"state"`
	Owner                  string           `json:"owner"`
	Submission             string           `json:"submission,omitempty"`
	Candidate              string           `json:"candidate,omitempty"`
	Conclusion             string           `json:"conclusion,omitempty"`
	Progress               string           `json:"progress,omitempty"`
	Blockers               []string         `json:"blockers"`
	BlockersOmitted        int              `json:"blockers_omitted,omitempty"`
	OpenQuestions          []string         `json:"open_questions"`
	QuestionsOmitted       int              `json:"questions_omitted,omitempty"`
	Evidence               []reportEvidence `json:"evidence"`
	FailingEvidence        int              `json:"failing_evidence"`
	EvidenceOmitted        int              `json:"evidence_omitted,omitempty"`
	EvidenceHistoryOmitted int              `json:"evidence_history_omitted,omitempty"`
}

func reportExcerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "… [truncated; task inspect for full text]"
}
