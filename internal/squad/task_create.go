package squad

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ShunL12324/c-squad/internal/preflight"
)

// createTask records creation inside the caller's transaction. Workspace creation
// remains outside that transaction in taskCommand, after the intent is committed.
func (st *Store) createTask(s *State, actor string, p []string, o options) (*Task, error) {
	if actor != "master" {
		return nil, fmt.Errorf("only master creates tasks: %w", ErrMasterRequired)
	}
	if len(p) < 2 || o["acceptance"] == "" {
		return nil, errors.New("title and --acceptance required")
	}
	if st.Generation > 0 && o["request-id"] == "" {
		return nil, errors.New("agents must provide a stable --request-id when creating a task")
	}
	if o["request-id"] != "" {
		for _, existing := range s.Tasks {
			if existing.RequestKey == actor+":"+o["request-id"] {
				if existing.Title != strings.Join(p[1:], " ") || existing.Acceptance != o["acceptance"] || existing.Description != o["description"] || (existing.Workspace != "") != (o["code"] == "true") {
					return nil, errors.New("request-id already used for different task content")
				}
				return existing, nil
			}
		}
	}
	dispatch := DispatchMode(o["dispatch"])
	if dispatch == "" {
		dispatch = DispatchModeAssigned
	}
	if dispatch != DispatchModeAssigned && dispatch != DispatchModeOpen {
		return nil, errors.New("--dispatch assigned|open required")
	}
	t := &Task{Dispatch: dispatch, Setup: o["setup"], ID: s.next("T"), Title: strings.Join(p[1:], " "), Description: o["description"], Acceptance: o["acceptance"], State: TaskPhaseReady, Updated: now(), Participants: []string{}, Dependencies: list(o["deps"]), Milestones: []Milestone{}, Evidence: []Evidence{}}
	if o["request-id"] != "" {
		t.RequestKey = actor + ":" + o["request-id"]
	}
	for _, d := range t.Dependencies {
		if s.Tasks[d] == nil {
			return nil, fmt.Errorf("unknown dependency %s", d)
		}
	}
	names := list(o["milestones"])
	for _, gate := range list(o["gates"]) {
		if !slices.Contains(names, gate) {
			return nil, fmt.Errorf("gate %s is not a milestone", gate)
		}
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			return nil, errors.New("duplicate milestone")
		}
		seen[name] = true
		t.Milestones = append(t.Milestones, Milestone{name, slices.Contains(list(o["gates"]), name), MilestoneStatePending})
	}
	if o["code"] == "true" {
		if err := preflight.Git(); err != nil {
			return nil, err
		}
		branch, e := git(s.Root, "symbolic-ref", "--short", "HEAD")
		if e != nil {
			return nil, errors.New("code tasks require a Git branch with a commit")
		}
		base, e := git(s.Root, "rev-parse", "HEAD")
		if e != nil {
			return nil, e
		}
		t.Base = base
		t.Target = branch
		t.Branch = "csquad/" + s.ID + "/" + t.ID
		t.Workspace = filepath.Join(st.Dir, "worktrees", t.ID)
		// Persist the workspace intent before invoking Git.
		t.State = TaskPhasePreparing
		printNotice([]string{codeTaskBinding(s, t)})
		noticeCrossRepoTeam(s, actor, t)
	}
	s.Tasks[t.ID] = t
	s.event(actor, "task_created", t.ID+" "+t.Title)
	for id, m := range s.Members {
		if t.State == TaskPhaseReady && t.Dispatch == DispatchModeOpen && id != "master" && m.State != MemberStateRemoved {
			notice := s.message(actor, id, t.ID, availableNotice(t), "")
			notice.Report = &ReportReference{Kind: "available"}
		}
	}
	return t, nil
}
