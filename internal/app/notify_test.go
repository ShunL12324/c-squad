package app

import (
	"testing"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/notify"
	"github.com/ShunL12324/c-squad/internal/store"
)

// Which event a task shows: never one while it works, and done only for a
// task without a flow.
func TestNoticeClass(t *testing.T) {
	plain := store.Task{Phase: store.Running, SessionID: "s"}
	flowed := store.Task{Phase: store.Running, SessionID: "s", CheckSpec: "codex"}
	with := func(t store.Task, phase string) store.Task { t.Phase = phase; return t }
	cases := []struct {
		name  string
		t     store.Task
		live  string
		class string
		phase bool
	}{
		{"working", plain, engine.Working, "", false},
		{"a question", plain, engine.NeedsInput, notify.NeedsInput, false},
		{"turn ended", plain, engine.Idle, notify.Done, false},
		{"session done", plain, engine.Done, notify.Done, false},
		{"session failed", plain, engine.Failed, notify.Failed, false},
		{"stopped by the user", plain, engine.Stopped, "", false},
		{"gone", plain, engine.Gone, "", false},
		{"flow: worker idle before the check", flowed, engine.Idle, "", false},
		{"flow: the flow moves a failed session", flowed, engine.Failed, "", false},
		{"flow: worker asks", flowed, engine.NeedsInput, notify.NeedsInput, false},
		{"checking", with(flowed, store.Checking), engine.Idle, "", false},
		{"fixing", with(flowed, store.Fixing), engine.Working, "", false},
		{"fixing, the worker asks back", with(flowed, store.Fixing), engine.NeedsInput, notify.NeedsInput, false},
		{"merging", with(flowed, store.Merging), engine.Idle, "", false},
		{"waiting", with(flowed, store.Waiting), "", "", false},
		{"ready", with(flowed, store.Ready), engine.Idle, notify.Ready, true},
		{"ready, a held message", with(flowed, store.Ready), engine.NeedsInput, notify.NeedsInput, false},
		{"needs you", with(flowed, store.NeedsYou), engine.Idle, notify.NeedsYou, true},
		{"phase failed", with(flowed, store.Failed), engine.Failed, notify.Failed, true},
	}
	for _, c := range cases {
		class := noticeClass(c.t, view(c.t, engine.Live{State: c.live}, true))
		if class != c.class || phaseEvent(c.t, class) != c.phase {
			t.Errorf("%s: %q (phase event %v), want %q (%v)", c.name, class, phaseEvent(c.t, class), c.class, c.phase)
		}
	}
	for phase, want := range map[string]string{store.Ready: notify.Ready, store.NeedsYou: notify.NeedsYou, store.Failed: notify.Failed,
		store.Fixing: "", store.Checking: "", store.Running: "", store.Cancelled: ""} {
		if got := phaseClass(with(flowed, phase)); got != want {
			t.Errorf("moved to %s: %q, want %q", phase, got, want)
		}
	}
	merged := with(flowed, store.Done)
	merged.MergeInto = "main"
	if phaseClass(merged) != notify.Merged || phaseClass(with(flowed, store.Done)) != "" {
		t.Error("done is merged only for a task that merges")
	}
}
