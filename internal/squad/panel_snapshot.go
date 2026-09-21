package squad

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/ShunL12324/c-squad/internal/teamui"
)

func (st *Store) panelSnapshot() (teamui.Snapshot, error) {
	s, err := st.read()
	if err != nil {
		return teamui.Snapshot{}, err
	}
	cfg, err := s.effectiveConfig()
	if err != nil {
		return teamui.Snapshot{}, err
	}
	out := teamui.Snapshot{Team: s.ID, Active: s.Active, Switch: switchHint(cfg)}
	taskIDs := sortedTaskIDs(s)
	for _, m := range navigationMembers(s) {
		tasks := []string{}
		for _, id := range taskIDs {
			t := s.Tasks[id]
			if t.State != TaskPhaseDone && (t.Owner == m.ID || slices.Contains(t.Participants, m.ID)) {
				tasks = append(tasks, id)
			}
		}
		card := teamui.Member{ID: m.ID, Engine: string(m.Engine), State: strings.ReplaceAll(string(m.State), "_", " "), Color: strings.TrimPrefix(m.Color.StyleValue(), "colour"), Cwd: displayDirectory(m.Cwd), Tasks: strings.Join(tasks, ", ")}
		// Resolve from the raw cwd, never from displayDirectory's abbreviation and
		// never from the task's workspace: in a cross-repository task the member
		// is on another repository's branch entirely.
		if state, ok := memberGit(m.Cwd); ok {
			card.Branch, card.Commit, card.Worktree = state.Branch, state.Commit, state.Worktree
		}
		out.Members = append(out.Members, card)
	}
	for _, id := range taskIDs {
		t := s.Tasks[id]
		color := "252"
		if owner := s.Members[t.Owner]; owner != nil {
			color = strings.TrimPrefix(owner.Color.StyleValue(), "colour")
		}
		workspace := t.Workspace
		if workspace == "" && s.Members[t.Owner] != nil {
			workspace = s.Members[t.Owner].Cwd
		}
		detail := fmt.Sprintf("With: %s\n\nGoal\n%s\n\nAcceptance\n%s\n\nLatest update\n%s\n\nWorkspace\n%s\n\nUpdated: %s", strings.Join(t.Participants, ", "), t.Description, t.Acceptance, t.Progress, workspace, t.Updated)
		if len(t.Blockers) > 0 {
			detail += "\n\nBlocked\n" + strings.Join(t.Blockers, "\n")
		}
		milestones := make([]teamui.Milestone, 0, len(t.Milestones))
		for _, ms := range t.Milestones {
			milestones = append(milestones, teamui.Milestone{Name: ms.Name, State: string(ms.State), Gate: ms.Gate})
		}
		for _, e := range t.Evidence {
			result := "failed"
			if e.Passed {
				result = "passed"
			}
			detail += fmt.Sprintf("\n\n%s · %s · %s\n%s", e.Member, e.Kind, result, e.Summary)
		}
		if t.Candidate != "" {
			detail += "\n\nCandidate: " + t.Candidate
		}
		if t.MergeCommit != "" {
			detail += "\n\nMerged: " + t.MergeCommit
		}
		// A task closed on outside evidence must never render like a merged one.
		note := ""
		if t.ExternalClosure != nil {
			note = "Closed externally · not merged · " + short(t.ExternalClosure.SHA)
			detail += "\n\n" + strings.Join(describeExternalClosure(t.ExternalClosure), "\n")
		}
		confirmation := ""
		canConfirm := t.State == TaskPhaseDone && t.UserConfirmation == nil
		if canConfirm {
			confirmation = "Awaiting user confirmation"
		}
		if t.UserConfirmation != nil {
			confirmation = "User confirmed"
			detail += "\n\nUser confirmation: " + t.UserConfirmation.At + " (" + t.UserConfirmation.Actor + ")"
		}
		out.Tasks = append(out.Tasks, teamui.Task{ID: t.ID, Title: t.Title, State: strings.ReplaceAll(string(t.State), "_", " "), Owner: t.Owner, Color: color, Progress: t.Progress, Detail: detail, Note: note, Confirmation: confirmation, CanConfirm: canConfirm, Milestones: milestones})
	}
	return out, nil
}

func sortedTaskIDs(s *State) []string {
	ids := make([]string, 0, len(s.Tasks))
	for id := range s.Tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.Tasks[ids[i]], s.Tasks[ids[j]]
		if (a.State == TaskPhaseDone) != (b.State == TaskPhaseDone) {
			return a.State != TaskPhaseDone
		}
		return ids[i] < ids[j]
	})
	return ids
}

// displayDirectory abbreviates only the user's home, preserving the actual cwd.
func displayDirectory(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}
