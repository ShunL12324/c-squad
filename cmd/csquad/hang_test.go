package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/proc"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

// hung makes the fake's `claude agents` never answer and ignore SIGTERM, as
// Claude Code 2.1.295's did when its background service stopped answering;
// csquad gives it a second.
var hung = []string{"FAKE_AGENTS_HANG=1", "CSQUAD_OBSERVE_TIMEOUT_MS=1000"}

// noneSurvive fails unless every never-answering process the fake started
// has gone (the agents call and the helper holding its stdout).
func noneSurvive(t *testing.T, h *testutil.Home) {
	t.Helper()
	pids := h.Hung()
	if len(pids) == 0 {
		t.Fatal("the fake recorded no hanging call")
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for proc.Running(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if proc.Running(pid) {
			t.Errorf("process %d survived", pid)
		}
	}
}

// timed runs csquad and fails when it takes longer than limit.
func timed(t *testing.T, h *testutil.Home, limit time.Duration, o opts, args ...string) result {
	t.Helper()
	start := time.Now()
	r := csq(t, h, o, args...)
	if took := time.Since(start); took > limit {
		t.Errorf("csquad %s took %v, more than %v", strings.Join(args, " "), took, limit)
	}
	return r
}

func TestStatusWhenClaudeAgentsDoesNotAnswer(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "job")
	writeTranscript(t, h, n, `{"type":"assistant","message":{"content":[{"type":"text","text":"halfway there"}]}}`)
	ok(t, csq(t, h, opts{env: mine()}, "status")) // the last good list
	r := ok(t, timed(t, h, 6*time.Second, opts{env: mine(hung...)}, "status"))
	for _, want := range []string{
		"Claude Code's background service did not answer",
		"claude daemon stop --any --keep-workers",
		"## T1 · job",
		"state: unknown: Claude Code's background service did not answer; last seen working",
		"halfway there",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q:\n%s", want, r.stdout)
		}
	}
	if lines := strings.Count(r.stdout, "claude daemon stop"); lines != 1 {
		t.Errorf("the fix is named %d times:\n%s", lines, r.stdout)
	}
	noneSurvive(t, h)

	// peek reads the transcript itself.
	r = ok(t, timed(t, h, 6*time.Second, opts{env: mine(hung...)}, "peek", "T1"))
	if !strings.Contains(r.stdout, "halfway there") || !strings.Contains(r.stdout, "unknown") {
		t.Errorf("peek:\n%s", r.stdout)
	}
	noneSurvive(t, h)
}

func TestFinishAndCancelChangeNothingWhenClaudeAgentsDoesNotAnswer(t *testing.T) {
	h := testutil.NewHome(t)
	n := launchAs(t, h, sid, "job")
	setState(h, n, func(s *testutil.Session) { s.State, s.Status = "done", "idle" })
	for _, cmd := range []string{"finish", "cancel"} {
		r := timed(t, h, 6*time.Second, opts{env: mine(hung...)}, cmd, "T1")
		if r.code == 0 || !strings.Contains(r.stderr, "T1 was left as it is") || !strings.Contains(r.stderr, "claude daemon stop --any --keep-workers") {
			t.Errorf("%s: exit %d\n%s%s", cmd, r.code, r.stdout, r.stderr)
		}
	}
	noneSurvive(t, h)
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; !strings.Contains(out, "## T1 · job") {
		t.Errorf("the task changed:\n%s", out)
	}
	if len(h.Sessions()) != 1 {
		t.Errorf("the session was removed: %v", h.Sessions())
	}
}

func TestWaitSaysOnceThatClaudeAgentsDoesNotAnswerAndWaits(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	// The engine-failure limit does not apply: the service may be restarted.
	env := append(mine(hung...), "CSQUAD_WAIT_INTERVAL_MS=200", "CSQUAD_WAIT_FAIL_MS=100")
	r := ok(t, timed(t, h, 15*time.Second, opts{env: env}, "wait", "T1", "--timeout", "0.1"))
	if !strings.Contains(r.stdout, "T1 · job · still working after") {
		t.Errorf("stdout: %s", r.stdout)
	}
	if n := strings.Count(r.stderr, "did not answer"); n != 1 || !strings.Contains(r.stderr, "Still waiting") {
		t.Errorf("told %d times:\n%s", n, r.stderr)
	}
	noneSurvive(t, h)
}

func TestStatuslineStartsNoSecondCallWhileOneRuns(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	if rows := statusline(t, h, sid); len(rows) != 1 || !strings.Contains(rows[0], "working  ") {
		t.Fatalf("%q", rows)
	}
	calls := h.Calls()
	first := make(chan []string, 1)
	go func() {
		r := csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100", "FAKE_AGENTS_HANG=1"}}, "statusline")
		first <- strings.Split(sgrRe.ReplaceAllString(r.stdout, ""), "\n")
	}()
	for deadline := time.Now().Add(10 * time.Second); h.Calls() == calls && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Calls() != calls+1 {
		t.Fatal("the first refresh did not call claude agents")
	}
	rows := statusline(t, h, sid, "FAKE_AGENTS_HANG=1")
	if h.Calls() != calls+1 {
		t.Errorf("a second refresh started another claude agents call (%d calls)", h.Calls()-calls)
	}
	// Both show the last good rows, marked stale.
	if len(rows) != 1 || !strings.Contains(rows[0], "T1 ✻ job") || !strings.Contains(rows[0], "working?") {
		t.Errorf("second: %q", rows)
	}
	if got := <-first; !strings.Contains(got[0], "working?") {
		t.Errorf("first: %q", got)
	}
	noneSurvive(t, h)
}

func TestAnOrphanedClaudeAgentsCallIsKilled(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	// A call whose csquad died two minutes ago: still hanging.
	cmd := exec.CommandContext(context.Background(), filepath.Join(h.Bin, "claude"), "agents", "--json", "--all")
	cmd.Env = h.Env("FAKE_AGENTS_HANG=1")
	proc.KillGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	for deadline := time.Now().Add(10 * time.Second); len(h.Hung()) < 2 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	proc.Track(filepath.Join(h.Csquad, "cache", "claude-agents"), cmd, "statusline", time.Now().Add(-2*time.Minute))
	ok(t, csq(t, h, opts{env: mine()}, "status"))
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the orphaned call still runs")
	}
	noneSurvive(t, h)
	if left, _ := os.ReadDir(filepath.Join(h.Csquad, "cache", "claude-agents")); len(left) != 0 {
		t.Errorf("records left: %v", left)
	}
}
