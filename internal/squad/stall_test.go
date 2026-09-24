package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

// stallStore is a running team with one in-progress task owned by a, where
// every member has just finished a turn and was observed doing so.
func stallStore(t *testing.T) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Phase = TeamPhaseRunning
		// Unregistered master: stall notices stay pending instead of reaching a
		// native session, and no tmux server is ever contacted.
		s.Socket = filepath.Join(t.TempDir(), "tmux.sock")
		for _, m := range s.Members {
			m.State = MemberStateIdle
		}
		s.Tasks["T1"] = &Task{ID: "T1", Title: "work", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a"}, Updated: "2026-09-24T00:00:00Z"}
		return nil
	}))
	return st
}

var allKnown = map[string]bool{"master": true, "a": true, "b": true}

// pass runs one runtime evaluation at the given offset from a fixed start.
func pass(t *testing.T, st *Store, w *stallTimer, at time.Duration, known map[string]bool) {
	t.Helper()
	must(t, st.checkStall(w, known, nil, time.Unix(1e9, 0).Add(at)))
}

// passes steps through [from, to] every two seconds, as the runtime loop does.
func passes(t *testing.T, st *Store, w *stallTimer, from, to time.Duration, known map[string]bool) {
	t.Helper()
	for at := from; at <= to; at += 2 * time.Second {
		pass(t, st, w, at, known)
	}
}

func stallNotices(t *testing.T, st *Store) []*Message {
	t.Helper()
	s, err := st.read()
	must(t, err)
	var out []*Message
	for _, m := range s.Messages {
		if m.Report != nil && m.Report.Kind == "stall" {
			out = append(out, m)
		}
	}
	return out
}

func edit(t *testing.T, st *Store, fn func(s *State)) {
	t.Helper()
	must(t, st.update(func(s *State) error { fn(s); return nil }))
}

func TestStallNoticeAfterFullQuietWindowOnly(t *testing.T) {
	st := stallStore(t)
	var w stallTimer
	passes(t, st, &w, 0, stallWindow-2*time.Second, allKnown)
	if n := stallNotices(t, st); len(n) != 0 {
		t.Fatal("notice before the window elapsed")
	}
	passes(t, st, &w, stallWindow, stallWindow+10*time.Second, allKnown)
	n := stallNotices(t, st)
	if len(n) != 1 || n[0].To != "master" || n[0].Task != "T1" || n[0].State != DeliveryStatePending {
		t.Fatalf("want one pending notice to master, got %+v", n)
	}
	if !strings.Contains(n[0].Text, "task inspect T1") || !strings.Contains(n[0].Text, "a (idle)") || !strings.Contains(n[0].Text, "does not mean the work failed or is complete") {
		t.Fatalf("notice text: %s", n[0].Text)
	}
	s, err := st.read()
	must(t, err)
	if s.Tasks["T1"].State != TaskPhaseInProgress || s.Tasks["T1"].Approval != nil || s.Tasks["T1"].Updated != "2026-09-24T00:00:00Z" {
		t.Fatal("the notice changed the task")
	}
	// No ACK, no progress: the same episode never notifies again, even over
	// many windows, and failed delivery keeps retrying the same message.
	passes(t, st, &w, stallWindow+12*time.Second, 3*stallWindow, allKnown)
	if n := stallNotices(t, st); len(n) != 1 || n[0].ID != stallNotices(t, st)[0].ID {
		t.Fatalf("episode notified %d times", len(n))
	}
}

// A short pause between turns, long work and any known wait restart or block
// the window.
func TestStallWindowRestartsOnActivityAndWaits(t *testing.T) {
	for name, set := range map[string]func(s *State){
		"working":        func(s *State) { s.Members["a"].State = MemberStateWorking },
		"starting":       func(s *State) { s.Members["a"].State = MemberStateStarting },
		"interrupted":    func(s *State) { s.Members["a"].State = MemberStateInterrupted },
		"waiting master": func(s *State) { s.Members["a"].State = MemberStateWaitingMaster },
		"native wait":    func(s *State) { s.Members["a"].State = "waiting_permission" },
		// Blockers are derived from the ledger; an unfinished dependency is one.
		"blocker": func(s *State) {
			s.Tasks["T0"] = &Task{ID: "T0", State: TaskPhaseReady}
			s.Tasks["T1"].Dependencies = []string{"T0"}
		},
		"gate": func(s *State) {
			s.Tasks["T1"].Milestones = []Milestone{{Name: "plan", Gate: true, State: MilestoneStateAwaitingApproval}}
		},
		"task question": func(s *State) {
			s.Questions["Q1"] = &Question{ID: "Q1", Member: "b", Task: "T1", State: QuestionStateOpen}
		},
		"member question": func(s *State) { s.Questions["Q1"] = &Question{ID: "Q1", Member: "a", State: QuestionStateOpen} },
		"in review":       func(s *State) { s.Tasks["T1"].State = TaskPhaseInReview },
		"awaiting merge":  func(s *State) { s.Tasks["T1"].State = TaskPhaseAwaitingMerge },
		"ready":           func(s *State) { s.Tasks["T1"].State = TaskPhaseReady },
		"done":            func(s *State) { s.Tasks["T1"].State = TaskPhaseDone },
		"team stopping":   func(s *State) { s.Phase = TeamPhaseStopping },
		"no participants": func(s *State) { s.Tasks["T1"].Owner, s.Tasks["T1"].Participants = "", nil },
		"removed member":  func(s *State) { s.Members["a"].State = MemberStateRemoved },
		"missing member":  func(s *State) { s.Tasks["T1"].Participants = []string{"a", "ghost"} },
		"deliverable queue": func(s *State) {
			s.Members["a"].EngineID, s.Members["a"].Peer = "session-a", filepath.Join(os.TempDir(), "csq-no-such-peer.sock")
			s.message("master", "a", "T1", "please continue", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := stallStore(t)
			var w stallTimer
			passes(t, st, &w, 0, stallWindow-10*time.Second, allKnown)
			edit(t, st, set)
			passes(t, st, &w, stallWindow-8*time.Second, 3*stallWindow, allKnown)
			if n := stallNotices(t, st); len(n) != 0 {
				t.Fatalf("notice despite %s", name)
			}
		})
	}
	// Working briefly and stopping again restarts the full window.
	st := stallStore(t)
	var w stallTimer
	passes(t, st, &w, 0, stallWindow-10*time.Second, allKnown)
	edit(t, st, func(s *State) { s.Members["a"].State = MemberStateWorking })
	pass(t, st, &w, stallWindow-8*time.Second, allKnown)
	edit(t, st, func(s *State) { s.Members["a"].State = MemberStateIdle })
	passes(t, st, &w, stallWindow-6*time.Second, 2*stallWindow-10*time.Second, allKnown)
	if n := stallNotices(t, st); len(n) != 0 {
		t.Fatal("the window did not restart after activity")
	}
	passes(t, st, &w, 2*stallWindow-8*time.Second, 2*stallWindow, allKnown)
	if n := stallNotices(t, st); len(n) != 1 {
		t.Fatalf("notices after a full second window: %d", len(n))
	}
}

// Crashed, stopped and failed members are certainly not working. Messages to
// them wait for a restart, so those cannot count as pending work.
func TestStallNoticeForCrashedParticipantWithPausedMessage(t *testing.T) {
	for _, state := range []MemberState{MemberStateCrashed, MemberStateStopped, MemberStateError} {
		st := stallStore(t)
		edit(t, st, func(s *State) {
			// Registered, so only the paused state keeps the message from
			// counting as deliverable work.
			s.Members["a"].EngineID, s.Members["a"].Peer = "session-a", filepath.Join(os.TempDir(), "csq-no-such-peer.sock")
			s.Members["a"].State = state
			s.Tasks["T1"].Participants = []string{"a", "b"}
			// An errored session still receives messages, so only crashed
			// and stopped members have deliveries paused.
			if deliveryPaused(state) {
				s.message("master", "a", "T1", "are you there?", "")
			}
		})
		var w stallTimer
		passes(t, st, &w, 0, stallWindow+2*time.Second, allKnown)
		if n := stallNotices(t, st); len(n) != 1 || !strings.Contains(n[0].Text, "a ("+string(state)+")") {
			t.Fatalf("%s: notices %+v", state, n)
		}
	}
}

// An unknown observation never counts as idle: a member the helper did not
// report, or a pass whose observation failed, restarts the window.
func TestStallUnknownObservationRestartsWindow(t *testing.T) {
	st := stallStore(t)
	var w stallTimer
	unknownA := map[string]bool{"master": true, "b": true}
	passes(t, st, &w, 0, 3*stallWindow, unknownA)
	if len(stallNotices(t, st)) != 0 {
		t.Fatal("notice for an unobserved member")
	}
	passes(t, st, &w, 3*stallWindow+2*time.Second, 4*stallWindow-10*time.Second, allKnown)
	must(t, st.checkStall(&w, allKnown, os.ErrDeadlineExceeded, time.Unix(1e9, 0).Add(4*stallWindow-8*time.Second)))
	passes(t, st, &w, 4*stallWindow-6*time.Second, 5*stallWindow-10*time.Second, allKnown)
	if len(stallNotices(t, st)) != 0 {
		t.Fatal("a failed observation did not restart the window")
	}
}

// A gap longer than stallGap between passes, or time going backwards, proves
// nothing about the time in between.
func TestStallClockGapsRestartWindow(t *testing.T) {
	st := stallStore(t)
	var w stallTimer
	passes(t, st, &w, 0, stallWindow/2, allKnown)
	// Suspend or a blocked runtime: the next pass is far later.
	passes(t, st, &w, stallWindow+stallGap, stallWindow+stallGap+10*time.Second, allKnown)
	if len(stallNotices(t, st)) != 0 {
		t.Fatal("a gap counted as observed quiet time")
	}
	// A backwards jump also restarts.
	var back stallTimer
	passes(t, st, &back, 10*stallWindow, 10*stallWindow+stallWindow/2, allKnown)
	passes(t, st, &back, 9*stallWindow, 9*stallWindow+stallWindow-10*time.Second, allKnown)
	if len(stallNotices(t, st)) != 0 {
		t.Fatal("a backwards clock jump counted as quiet time")
	}
}

// Progress and new incarnations start a new episode; inspect does not.
func TestStallEpisodes(t *testing.T) {
	st := stallStore(t)
	var w stallTimer
	passes(t, st, &w, 0, stallWindow, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("first episode not notified")
	}
	// Reading the task is not progress.
	must(t, taskCommand(st, "master", []string{"inspect", "T1"}, options{}))
	passes(t, st, &w, stallWindow+2*time.Second, 2*stallWindow+4*time.Second, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("task inspect started a new episode")
	}
	// Real progress: a new episode, notified only after its own full window.
	must(t, taskCommand(st, "a", []string{"progress", "T1"}, options{"text": "half done"}))
	passes(t, st, &w, 2*stallWindow+6*time.Second, 3*stallWindow, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("new episode notified before its window")
	}
	passes(t, st, &w, 3*stallWindow+2*time.Second, 3*stallWindow+12*time.Second, allKnown)
	if len(stallNotices(t, st)) != 2 {
		t.Fatal("progress did not start a new episode")
	}
	// A restarted member (new generation) is also a new episode.
	edit(t, st, func(s *State) { s.Members["a"].Generation++ })
	passes(t, st, &w, 3*stallWindow+14*time.Second, 4*stallWindow+16*time.Second, allKnown)
	if len(stallNotices(t, st)) != 3 {
		t.Fatal("a new generation did not start a new episode")
	}
}

// The runtime restarting (or a team resume) loses the timer, never the record:
// a notified episode is not repeated and an unnotified one needs a full window.
func TestStallRuntimeRestart(t *testing.T) {
	st := stallStore(t)
	var first stallTimer
	passes(t, st, &first, 0, stallWindow, allKnown)
	var second stallTimer
	passes(t, st, &second, 0, 3*stallWindow, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("a restarted runtime repeated the notice")
	}
	edit(t, st, func(s *State) {
		s.Tasks["T2"] = &Task{ID: "T2", State: TaskPhaseInProgress, Owner: "b", Participants: []string{"b"}}
	})
	var early stallTimer
	passes(t, st, &early, 0, stallWindow/2, allKnown)
	var third stallTimer
	passes(t, st, &third, 0, stallWindow-2*time.Second, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("time observed by a previous runtime was carried over")
	}
}

// A notice queued for an episode that ends before delivery is superseded on
// the existing report path and never transported.
func TestStallNoticeExpiresBeforeDelivery(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, st *Store){
		"member resumed": func(t *testing.T, st *Store) {
			edit(t, st, func(s *State) { s.Members["a"].State = MemberStateWorking })
		},
		"progress": func(t *testing.T, st *Store) {
			must(t, taskCommand(st, "a", []string{"progress", "T1"}, options{"text": "back"}))
		},
		"completed": func(t *testing.T, st *Store) { edit(t, st, func(s *State) { s.Tasks["T1"].State = TaskPhaseDone }) },
		"question": func(t *testing.T, st *Store) {
			edit(t, st, func(s *State) {
				s.Questions["Q9"] = &Question{ID: "Q9", Member: "a", Task: "T1", State: QuestionStateOpen}
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := stallStore(t)
			var w stallTimer
			passes(t, st, &w, 0, stallWindow, allKnown)
			change(t, st)
			edit(t, st, func(s *State) { s.expireReports() })
			n := stallNotices(t, st)
			if len(n) != 1 || n[0].State != DeliveryStateSuperseded {
				t.Fatalf("notice after %s: %+v", name, n)
			}
			must(t, st.deliver(n[0].ID))
			if stallNotices(t, st)[0].State != DeliveryStateSuperseded {
				t.Fatal("a superseded notice was transported")
			}
		})
	}
}

// Codex reports an interrupted turn; the member is paused, not idle, until
// its next prompt.
func TestInterruptHookPausesMember(t *testing.T) {
	st := testStore(t)
	for event, want := range map[string]MemberState{"Interrupt": MemberStateInterrupted, "UserPromptSubmit": MemberStateWorking} {
		must(t, hookInput(st, "a", 1, strings.NewReader(`{"hook_event_name":"`+event+`"}`)))
		s, err := st.read()
		must(t, err)
		if s.Members["a"].State != want {
			t.Fatalf("after %s: %s, want %s", event, s.Members["a"].State, want)
		}
	}
}

// observe must report a Claude member as known only when the agents helper
// answered for its session; a failed helper leaves the old state unproven.
func TestObserveReportsOnlyVerifiedMembers(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "csq-obs-")
	must(t, err)
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	_, err = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "keep", "sleep", "60")
	must(t, err)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	listing := filepath.Join(dir, "claude-ok")
	must(t, os.WriteFile(listing, []byte("#!/bin/sh\necho '[{\"sessionId\":\"s-b\",\"status\":\"idle\"}]'\n"), 0700))
	failing := filepath.Join(dir, "claude-fail")
	must(t, os.WriteFile(failing, []byte("#!/bin/sh\nexit 1\n"), 0700))
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Profiles = map[string]config.Profile{
			"ok":   {Engine: config.Claude, Command: &config.Command{Executable: listing}},
			"fail": {Engine: config.Claude, Command: &config.Command{Executable: failing}},
		}
		s.Config, s.Socket = &cfg, socket
		for id, m := range s.Members {
			m.Session, m.State = "obs-"+id, MemberStateIdle
		}
		s.Members["master"].Engine = config.Codex
		s.Members["a"].Profile, s.Members["a"].EngineID = "fail", "s-a"
		s.Members["b"].Profile, s.Members["b"].EngineID = "ok", "s-b"
		return nil
	}))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Members {
		_, err = tm(s, "new-session", "-d", "-s", m.Session, "sleep", "60")
		must(t, err)
	}
	known, err := st.observe()
	must(t, err)
	if !known["master"] || known["a"] || !known["b"] {
		t.Fatalf("known = %v, want master (codex pane) and b (listed) only", known)
	}
	_, err = tm(s, "kill-session", "-t", "=obs-a")
	must(t, err)
	known, err = st.observe()
	must(t, err)
	if !known["a"] {
		t.Fatal("a crashed member is a known fact")
	}
}

// A task that changes between the pass that found its window expired and the
// transaction that queues the notice is a new episode: it gets no notice until
// it has been quiet for a window of its own.
func TestStallNoticeRequiresTheTimedEpisode(t *testing.T) {
	st := stallStore(t)
	var w stallTimer
	passes(t, st, &w, 0, stallWindow-2*time.Second, allKnown)
	s, err := st.read()
	must(t, err)
	due := w.due(s, allKnown, time.Unix(1e9, 0).Add(stallWindow))
	if len(due) != 1 || due[0].task != "T1" {
		t.Fatalf("due = %+v", due)
	}
	// Master records progress on the task while its members stay idle.
	must(t, taskCommand(st, "master", []string{"progress", "T1"}, options{"text": "reassessing"}))
	queued, err := st.notifyStall(due, allKnown)
	must(t, err)
	if queued || len(stallNotices(t, st)) != 0 {
		t.Fatal("the changed episode was notified without its own window")
	}
	passes(t, st, &w, stallWindow+2*time.Second, 2*stallWindow, allKnown)
	if len(stallNotices(t, st)) != 0 {
		t.Fatal("the new episode was notified early")
	}
	passes(t, st, &w, 2*stallWindow+2*time.Second, 2*stallWindow+6*time.Second, allKnown)
	if len(stallNotices(t, st)) != 1 {
		t.Fatal("the new episode was not notified after its window")
	}
}

// A queued notice is only delivered while the runtime still observes the
// stall. If observation fails, a member works briefly, or a new runtime has
// observed nothing yet, it is superseded before delivery, and a later fully
// observed window of the same episode can notify again. A delivered notice is
// never repeated.
func TestStallNoticeNeedsCurrentObservationToDeliver(t *testing.T) {
	for name, interrupt := range map[string]func(t *testing.T, st *Store, w *stallTimer, at time.Duration){
		"failed observation": func(t *testing.T, st *Store, w *stallTimer, at time.Duration) {
			must(t, st.checkStall(w, allKnown, os.ErrDeadlineExceeded, time.Unix(1e9, 0).Add(at)))
		},
		"unobserved member": func(t *testing.T, st *Store, w *stallTimer, at time.Duration) {
			pass(t, st, w, at, map[string]bool{"master": true, "b": true})
		},
		"brief work without progress": func(t *testing.T, st *Store, w *stallTimer, at time.Duration) {
			edit(t, st, func(s *State) { s.Members["a"].State = MemberStateWorking })
			pass(t, st, w, at, allKnown)
			edit(t, st, func(s *State) { s.Members["a"].State = MemberStateIdle })
		},
		"runtime restart": func(t *testing.T, st *Store, w *stallTimer, at time.Duration) {
			*w = stallTimer{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := stallStore(t)
			var w stallTimer
			passes(t, st, &w, 0, stallWindow, allKnown)
			must(t, st.expireStall(&w))
			if n := stallNotices(t, st); len(n) != 1 || n[0].State != DeliveryStatePending {
				t.Fatalf("a backed notice was expired: %+v", n)
			}
			interrupt(t, st, &w, stallWindow+2*time.Second)
			must(t, st.expireStall(&w))
			n := stallNotices(t, st)
			if len(n) != 1 || n[0].State != DeliveryStateSuperseded {
				t.Fatalf("notice after %s: %+v", name, n)
			}
			must(t, st.deliver(n[0].ID))
			if stallNotices(t, st)[0].State != DeliveryStateSuperseded {
				t.Fatal("an unconfirmed notice was transported")
			}
			// The same episode, observed quiet for a whole new window.
			passes(t, st, &w, stallWindow+4*time.Second, 2*stallWindow+6*time.Second, allKnown)
			if n = stallNotices(t, st); len(n) != 2 || n[1].State != DeliveryStatePending {
				t.Fatalf("a superseded notice swallowed the next window: %+v", n)
			}
			// Once delivered, the episode is never notified again.
			edit(t, st, func(s *State) { s.Messages[len(s.Messages)-1].State = DeliveryStateSent })
			interrupt(t, st, &w, 2*stallWindow+8*time.Second)
			passes(t, st, &w, 2*stallWindow+10*time.Second, 4*stallWindow, allKnown)
			if n = stallNotices(t, st); len(n) != 2 {
				t.Fatalf("a delivered episode was notified again: %d notices", len(n))
			}
		})
	}
}
