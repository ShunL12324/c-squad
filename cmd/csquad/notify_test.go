package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/store"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

func notes(h *testutil.Home) string {
	var out []string
	for _, n := range h.Notifications() {
		out = append(out, n[0]+" | "+n[1])
	}
	return strings.Join(out, "\n")
}

func assistant(text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]}}`, text)
}

// A task without a flow: done when its turn ends, needs input on a
// permission prompt, each once, whatever observes it and however often;
// nothing while it works.
func TestNotificationsOfAPlainTask(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "fix login redirect")
	statusline(t, h, sid)
	if got := notes(h); got != "" {
		t.Fatalf("a working task: %s", got)
	}
	writeTranscript(t, h, n, assistant("## Fixed the redirect\nDetails follow."))
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	statusline(t, h, sid)
	statusline(t, h, sid)
	ok(t, csq(t, h, opts{env: mine()}, "status"))
	if got, want := notes(h), "T1 · fix login redirect | done · Fixed the redirect"; got != want {
		t.Fatalf("done:\n%s\nwant\n%s", got, want)
	}
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "working", "busy" })
	statusline(t, h, sid)
	writeTranscript(t, h, n, assistant("May I run the migration?\nIt drops a table."))
	setState(h, n, func(s *testutil.Session) { s.Status, s.WaitingFor = "waiting", "permission prompt" })
	// Concurrent observers: five status lines and two commands.
	var wg sync.WaitGroup
	for i := range 7 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i < 5 {
				csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100"}}, "statusline")
			} else {
				csq(t, h, opts{env: mine()}, "status")
			}
		}()
	}
	wg.Wait()
	want := "T1 · fix login redirect | done · Fixed the redirect\nT1 · fix login redirect | needs input · May I run the migration?"
	if got := notes(h); got != want {
		t.Fatalf("needs input:\n%s\nwant\n%s", got, want)
	}
}

// CSQUAD_NOTIFY and csquad notify on|input|off.
func TestNotifyModes(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "x")
	writeTranscript(t, h, n, assistant("All done."))
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	statusline(t, h, sid, "CSQUAD_NOTIFY=off")
	statusline(t, h, sid, "CSQUAD_NOTIFY=input")
	if got := notes(h); got != "" {
		t.Fatalf("off, then input, sent done: %s", got)
	}
	setState(h, n, func(s *testutil.Session) { s.Status, s.WaitingFor = "waiting", "permission prompt" })
	statusline(t, h, sid, "CSQUAD_NOTIFY=input")
	if got := notes(h); got != "T1 · x | needs input · All done." {
		t.Fatalf("input: %s", got)
	}

	if out := ok(t, csq(t, h, opts{}, "notify", "off")).stdout; !strings.Contains(out, "Notifications: off") {
		t.Errorf("notify off: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(h.Csquad, "notify")); strings.TrimSpace(string(b)) != "off" {
		t.Errorf("the mode file holds %q", b)
	}
	setState(h, n, func(s *testutil.Session) { s.State, s.Status, s.WaitingFor = "working", "busy", nil })
	statusline(t, h, sid)
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	statusline(t, h, sid)
	if got := len(h.Notifications()); got != 1 {
		t.Errorf("notify off: %d notifications", got)
	}
	out := ok(t, csq(t, h, opts{env: []string{"CSQUAD_NOTIFY=on"}}, "notify", "input")).stdout
	if !strings.Contains(out, "Notifications: input (needs input and needs you only)") || !strings.Contains(out, "CSQUAD_NOTIFY=on is set here, and the variable wins") {
		t.Errorf("notify input: %s", out)
	}
	out = ok(t, csq(t, h, opts{}, "notify")).stdout
	if !strings.Contains(out, "Notifications: input") || !strings.Contains(out, "Backend: file "+h.NotifyFile()) {
		t.Errorf("notify: %s", out)
	}
	out = ok(t, csq(t, h, opts{}, "notify", "test")).stdout
	if !strings.Contains(out, "Sent a test notification through file ") {
		t.Errorf("notify test: %s", out)
	}
	if last := h.Notifications()[len(h.Notifications())-1]; last[0] != "csquad · test" {
		t.Errorf("the test notification: %v", last)
	}
	if r := csq(t, h, opts{}, "notify", "loud"); r.code != 2 || !strings.Contains(r.stderr, "usage: csquad notify") {
		t.Errorf("notify loud: %+v", r)
	}
}

// The flow's phases: ready once (also after a held message), merged, and
// needs you with its reason.
func TestNotificationsOfTheFlow(t *testing.T) {
	h, repo := flowRepo(t)
	n := launchIn(t, h, repo, "fix", "--check", "cmd:true")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	advance(t, h)
	waitPhase(t, h, n, store.Ready)
	waitUntil(t, "ready", 10*time.Second, func() bool { return len(h.Notifications()) == 1 })
	label := fmt.Sprint(n)
	setState(h, label, func(s *testutil.Session) { s.Status, s.WaitingFor = "waiting", "permission prompt" })
	statusline(t, h, sid)
	setState(h, label, func(s *testutil.Session) { s.Status, s.WaitingFor = "idle", nil })
	statusline(t, h, sid)
	advance(t, h)
	if got, want := notes(h), "T1 · fix | ready\nT1 · fix | needs input"; got != want {
		t.Fatalf("ready:\n%s\nwant\n%s", got, want)
	}

	m := launchIn(t, h, repo, "merge it", "--merge")
	work(t, h, repo, m, map[string]string{"c.txt": "c\n"})
	advance(t, h)
	waitPhase(t, h, m, store.Done)
	if got := h.Notifications(); len(got) != 3 || got[2] != [2]string{"T2 · merge it", "merged · into main"} {
		t.Fatalf("merged: %v", got)
	}

	k := launchIn(t, h, repo, "unproven", "--merge")
	setState(h, fmt.Sprint(k), func(s *testutil.Session) { s.State, s.Status = "working", "idle" })
	advance(t, h)
	waitPhase(t, h, k, store.NeedsYou)
	got := h.Notifications()
	if len(got) != 4 || got[3][0] != "T3 · unproven" || !strings.HasPrefix(got[3][1], "needs you · merge stopped: no branch of T3 found") {
		t.Fatalf("needs you: %v", got)
	}
	statusline(t, h, sid)
	advance(t, h)
	if len(h.Notifications()) != 4 {
		t.Errorf("needs you again: %v", h.Notifications())
	}
}

// The Stop hook of a task without a flow starts an advance, which sees the
// turn end: done, also without a status line.
func TestStopHookOfAPlainTaskNotifiesDone(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "plain")
	writeTranscript(t, h, n, assistant("Result: ok."))
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	full := h.Sessions()[0].SessionID
	ok(t, csq(t, h, opts{stdin: `{"session_id":"` + full + `"}`, env: []string{"CSQUAD_STOP_POLL_MS=20"}}, "hook", "claude-stop"))
	waitUntil(t, "done", 10*time.Second, func() bool { return notes(h) == "T1 · plain | done · Result: ok." })
	waitUntil(t, "the advance", 10*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(h.Csquad, "logs", "advance.log"))
		return strings.Contains(string(b), "csquad advance --stopped T1")
	})
	time.Sleep(300 * time.Millisecond) // the advance exits
}
