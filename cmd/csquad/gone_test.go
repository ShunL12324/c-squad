package main

import (
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/store"
)

// A task whose session, worktree and branch were deleted by hand is taken
// out of the list, by status and by the status line.
func TestGoneTaskWithNothingLeftIsCleared(t *testing.T) {
	for _, via := range []string{"status", "statusline"} {
		h, repo, wt := repoWithTask(t)
		git(t, h, repo, "worktree", "remove", "--force", "--force", wt)
		git(t, h, repo, "branch", "-D", "worktree-t1-fix")
		h.SetSessions(nil)
		if via == "status" {
			if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; strings.Contains(out, "T1") {
				t.Errorf("status: %s", out)
			}
		} else if rows := statusline(t, h, sid); len(rows) != 0 {
			t.Errorf("status line: %q", rows)
		}
		if task := getTask(t, h, 1); task.Phase != store.Cancelled || task.Note != "removed outside csquad" {
			t.Errorf("%s: %+v", via, task)
		}
		if ev := strings.Join(events(h), "\n"); !strings.Contains(ev, "T1 running→cancelled") {
			t.Errorf("%s: events %s", via, ev)
		}
	}
}

// A gone task with a worktree or branch left stays, gone, with the command
// that clears it; cancel keeps the work and says where, --discard deletes it.
func TestGoneTaskWithLeftoversStaysUntilCancel(t *testing.T) {
	h, repo, wt := repoWithTask(t)
	h.SetSessions(nil)
	out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout
	if !strings.Contains(out, "state: gone ") || !strings.Contains(out, "hint: the session was removed outside csquad; csquad cancel T1 clears it") {
		t.Errorf("status: %s", out)
	}
	if rows := statusline(t, h, sid); len(rows) != 1 || !strings.Contains(rows[0], "T1") || !strings.Contains(rows[0], "gone") {
		t.Errorf("status line: %q", rows)
	}
	if task := getTask(t, h, 1); task.Phase != store.Running {
		t.Errorf("cleared with work left: %+v", task)
	}
	r := ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1"))
	if !strings.Contains(r.stdout, "cancelled T1 · fix · nothing") || !strings.Contains(r.stdout, "kept its work (worktree "+wt+", branch worktree-t1-fix): branch worktree-t1-fix is not merged into main") {
		t.Errorf("cancel: %s", r.stdout)
	}
	if !exists(wt) {
		t.Error("cancel removed unmerged work")
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; strings.Contains(out, "T1") {
		t.Errorf("still listed: %s", out)
	}
	r = ok(t, csq(t, h, opts{env: mine()}, "cancel", "T1", "--discard"))
	if !strings.Contains(r.stdout, "discarded T1 · fix · worktree "+wt+", branch worktree-t1-fix") || exists(wt) || strings.Contains(git(t, h, repo, "branch"), "worktree-t1-fix") {
		t.Errorf("--discard: %s", r.stdout)
	}
}
