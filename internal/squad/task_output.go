package squad

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// taskCell keeps the table useful at a terminal width while preserving the
// complete record through --output json. Control characters cannot add rows.
func taskCell(value string, width int) string {
	return ansi.Truncate(strings.Join(strings.Fields(ansi.Strip(value)), " "), width, "…")
}

type currentEvidence struct {
	index int
	value Evidence
}

func writeTaskTable(out io.Writer, t *Task) error {
	if t == nil {
		_, err := fmt.Fprintln(out, "No task")
		return err
	}
	var lines []string
	add := func(label, value string) {
		if label == "" {
			lines = append(lines, "  "+value)
			return
		}
		lines = append(lines, label+": "+value)
	}
	add("Task", t.ID+" "+taskCell(t.Title, 88))
	add("State", string(t.State)+" | owner "+taskValue(t.Owner)+" | dispatch "+string(t.Dispatch))
	if t.Submission != "" {
		add("Submission", t.Submission+" | candidate "+taskValue(t.Candidate))
	} else if t.Candidate != "" {
		add("Candidate", t.Candidate)
	}
	if t.Workspace != "" {
		add("Workspace", taskCell(t.Workspace, 220))
	}
	if t.Branch != "" {
		add("Branch", taskCell(t.Branch, 100)+" -> "+taskValue(t.Target))
	}
	if len(t.Dependencies) > 0 {
		add("Depends", taskCell(strings.Join(t.Dependencies, ", "), 180))
	}
	add("Scope", taskCell(t.Description, 200))
	add("Acceptance", taskCell(t.Acceptance, 200))
	if t.Setup != "" {
		add("Setup", taskCell(t.Setup, 180))
	}
	if t.Progress != "" {
		add("Progress", taskCell(t.Progress, 200))
	}
	if t.SubmissionSummary != "" {
		add("Conclusion", taskCell(t.SubmissionSummary, 200))
	}
	if len(t.Blockers) == 0 {
		add("Blockers", "none")
	} else {
		add("Blockers", fmt.Sprintf("%d", len(t.Blockers)))
		for i, blocker := range t.Blockers {
			if i >= 4 {
				add("", fmt.Sprintf("+%d more blockers", len(t.Blockers)-i))
				break
			}
			add("", "- "+taskCell(blocker, 160))
		}
	}
	gateCount := 0
	for _, milestone := range t.Milestones {
		if milestone.Gate && milestone.State == MilestoneStateAwaitingApproval {
			gateCount++
		}
	}
	add("Gates", fmt.Sprintf("%d awaiting approval", gateCount))
	// Waiting gates lead the bounded list; pending/reporting milestones follow.
	milestones := append([]Milestone(nil), t.Milestones...)
	sort.SliceStable(milestones, func(i, j int) bool {
		waiting := func(m Milestone) bool { return m.Gate && m.State == MilestoneStateAwaitingApproval }
		return waiting(milestones[i]) && !waiting(milestones[j])
	})
	for i, milestone := range milestones {
		if i >= 8 {
			add("", fmt.Sprintf("+%d more milestones", len(milestones)-i))
			break
		}
		gate := ""
		if milestone.Gate {
			gate = " [gate]"
		}
		add("", "- "+taskCell(milestone.Name, 100)+gate+" "+string(milestone.State))
	}
	latest := map[string]currentEvidence{}
	for i, evidence := range t.Evidence {
		if t.Submission != "" && evidence.Submission == t.Submission && evidence.SHA == t.Candidate {
			latest[evidence.Member+":"+string(evidence.Kind)] = currentEvidence{i + 1, evidence}
		}
	}
	entries := make([]currentEvidence, 0, len(latest))
	failed := 0
	for _, entry := range latest {
		entries = append(entries, entry)
		if !entry.value.Passed {
			failed++
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value.Passed != entries[j].value.Passed {
			return !entries[i].value.Passed
		}
		return entries[i].index < entries[j].index
	})
	add("Evidence", fmt.Sprintf("%d current latest; %d failing; %d total records", len(entries), failed, len(t.Evidence)))
	for i, entry := range entries {
		if i >= 8 {
			add("", fmt.Sprintf("+%d more current evidence entries (including %d unshown failures)", len(entries)-i, max(0, failed-i)))
			break
		}
		result := "PASS"
		if !entry.value.Passed {
			result = "FAIL"
		}
		add("", fmt.Sprintf("- #%d %s %s %s: %s", entry.index, result, entry.value.Kind, taskCell(entry.value.Member, 50), taskCell(entry.value.Summary, 150)))
	}
	if t.Approval != nil {
		add("Approval", "by "+taskCell(t.Approval.By, 50)+" at candidate "+taskCell(t.Approval.SHA, 64))
	}
	if t.MergeCommit != "" {
		add("Merged", t.MergeCommit)
	}
	if t.Cancellation != nil {
		add("Cancelled", taskCell(t.Cancellation.Reason, 160))
	}
	if t.ExternalClosure != nil {
		add("External", taskCell(t.ExternalClosure.Repo, 140)+" @ "+taskCell(t.ExternalClosure.SHA, 64))
		add("Limits", taskCell(t.ExternalClosure.Limits, 160))
	}
	add("Next", taskNext(t, gateCount, failed))
	lines = append(lines, "Full record: csquad task inspect "+t.ID+" --output json")
	_, err := io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}

func taskValue(value string) string {
	if value == "" {
		return "none"
	}
	return taskCell(value, 100)
}

func taskNext(t *Task, pendingGates, failingEvidence int) string {
	switch {
	case t.State == TaskPhaseCancelled || t.State == TaskPhaseDone:
		return "closed; inspect full record for history"
	case len(t.Blockers) > 0:
		return "resolve recorded blockers"
	case pendingGates > 0:
		return "master decision on awaiting gate"
	case failingEvidence > 0:
		return "resolve current failing evidence"
	case t.State == TaskPhaseInReview:
		return "review current submission and evidence before approval"
	case t.State == TaskPhaseAwaitingMerge:
		return "master merge of approved candidate pending"
	case t.State == TaskPhaseReady:
		return "claim or assign task"
	case t.State == TaskPhaseInProgress:
		return "owner continues work or submits candidate when ready"
	default:
		return "inspect current phase and full record"
	}
}
