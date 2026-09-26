// Run with: go run -tags t541preview ./prototypes/t541
// The build-tagged renderer calls the production sidePanel layout directly.
package main

import (
	"fmt"
	"os"

	"github.com/ShunL12324/c-squad/internal/teamui"
)

func main() {
	dir := "prototypes/t541"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	screenshot := teamui.Snapshot{Team: "csquad-1", Active: true, Members: []teamui.Member{
		{ID: "master", Engine: "codex", State: "idle", Color: "87", Branch: "main", Cwd: "~/projects/c-squad"},
		{ID: "navigation-dev", Engine: "codex", State: "idle", Color: "117", Branch: "csquad/T197", Worktree: true, Cwd: "…/csquad-1/worktrees/T197"},
		{ID: "observation-dev", Engine: "codex", State: "idle", Color: "214", Branch: "csquad/T29", Worktree: true, Cwd: "…/csquad-1/worktrees/T29"},
	}, Tasks: []teamui.Task{
		{ID: "T1", Title: "Publish v0.12.0 and investigate C-Squad execution overhead", State: "done", Owner: "master", Color: "87", Completion: "Published and verified v0.12.0 plus six default profiles. Completed native conversation and tool-call validation.", Milestones: []teamui.Milestone{{Name: "Build", State: "approved"}, {Name: "Review", State: "approved"}, {Name: "Publish", State: "approved"}}},
		{ID: "T191", Title: "Publish v0.12.1 coordinated validation improvements", State: "done", Owner: "master", Color: "87", Completion: "Published v0.12.1 from the reviewed candidate, with signed package validation and source checks.", Milestones: []teamui.Milestone{{Name: "Review", State: "approved"}, {Name: "Validate", State: "approved"}, {Name: "Publish", State: "approved"}}},
	}}
	// The screenshot has 28 Done tasks. Keep its first two visible entries, then
	// populate the remaining count so the preview exercises real pagination and
	// shows how many complete cards fit at the actual 40×46 task pane size.
	for i := 2; i < 28; i++ {
		screenshot.Tasks = append(screenshot.Tasks, teamui.Task{
			ID: fmt.Sprintf("T%d", i), Title: "Validate native navigation and publication workflow", State: "done",
			Owner: "master", Color: "87", Completion: "Accepted source and package checks after native panel validation.",
			Milestones: []teamui.Milestone{{Name: "Review", State: "approved"}, {Name: "Validation", State: "approved"}},
		})
	}
	active := teamui.Snapshot{Team: "csquad-1", Active: true, Members: []teamui.Member{
		{ID: "master", Engine: "codex", State: "idle", Color: "87", Branch: "main", Cwd: "~/projects/c-squad"},
		{ID: "navigation-dev", Engine: "codex", State: "working", Color: "117", Tasks: "T541, T543", Branch: "csquad/T541", Worktree: true, Cwd: "…/csquad-1/worktrees/T541"},
		{ID: "observation-dev", Engine: "codex", State: "idle", Color: "214", Branch: "csquad/T29", Worktree: true, Cwd: "…/csquad-1/worktrees/T29"},
		{ID: "review-dev", Engine: "codex", State: "blocked", Color: "211", Tasks: "T544", Branch: "csquad/T544", Worktree: true, Cwd: "…/csquad-1/worktrees/T544"},
	}, Tasks: []teamui.Task{
		{ID: "T541", Title: "Refine member and task panels from the latest screenshot", State: "in progress", Owner: "navigation-dev", Color: "117", Progress: "Borderless surfaces and persistent teammate color now drive the visual hierarchy. Full implementation review is next.", Milestones: []teamui.Milestone{{Name: "Design", State: "approved"}, {Name: "Implement", State: "reported"}, {Name: "Validate", State: "pending"}}},
		{ID: "T543", Title: "Review side panel interaction and responsive layout", State: "review", Owner: "observation-dev", Color: "214", Progress: "Read-only review checks card focus and button hit targets before native validation.", Milestones: []teamui.Milestone{{Name: "Inspect", State: "approved"}, {Name: "Review", State: "awaiting_approval", Gate: true}}},
		{ID: "T544", Title: "Resolve blocked package publication request", State: "blocked", Owner: "review-dev", Color: "211", Note: "Waiting for registry access before publication resumes."},
		{ID: "T531", Title: "Publish and verify v0.12.6", State: "done", Owner: "master", Color: "87", Completion: "Public verifier passed release notes, seven GitHub checksums, npm integrity, four native binaries, Homebrew, and APT.", Milestones: []teamui.Milestone{{Name: "Publish", State: "approved"}, {Name: "Verify", State: "approved"}}},
		{ID: "T470", Title: "Earlier release candidate", State: "cancelled", Owner: "review-dev", Color: "211", Note: "Superseded by the verified v0.12.6 source."},
	}}
	cases := []struct {
		name, kind, current, selected string
		width, height                 int
		data                          teamui.Snapshot
		completed                     bool
		detailID                      string
	}{
		{"members-screenshot-28x46", "members", "master", "master", 28, 46, screenshot, false, ""},
		{"tasks-screenshot-40x46", "tasks", "master", "T1", 40, 46, screenshot, true, ""},
		{"members-28x46", "members", "master", "navigation-dev", 28, 46, active, false, ""},
		{"members-32x46", "members", "master", "navigation-dev", 32, 46, active, false, ""},
		{"members-28x12", "members", "master", "navigation-dev", 28, 12, active, false, ""},
		{"tasks-active-40x46", "tasks", "master", "T541", 40, 46, active, false, ""},
		{"tasks-review-40x46", "tasks", "master", "T543", 40, 46, active, false, ""},
		{"tasks-blocked-40x46", "tasks", "master", "T544", 40, 46, active, false, ""},
		{"tasks-done-40x46", "tasks", "master", "T531", 40, 46, active, true, ""},
		{"tasks-active-40x12", "tasks", "master", "T541", 40, 12, active, false, ""},
		{"task-detail-40x46", "tasks", "master", "T541", 40, 46, active, false, "T541"},
	}
	for _, example := range cases {
		if err := teamui.SaveT541Preview(dir, example.name, example.kind, example.current, example.selected,
			example.width, example.height, example.data, example.completed, example.detailID); err != nil {
			fmt.Fprintln(os.Stderr, example.name+":", err)
			os.Exit(1)
		}
		fmt.Println(example.name)
	}
}
