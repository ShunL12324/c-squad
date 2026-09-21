package squad

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/prompts"
)

func briefStore(t *testing.T, phase TaskPhase) *Store {
	t.Helper()
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", Title: "Wire the panel", Owner: "a", State: phase, Updated: now(),
			Participants: []string{"a"}, Milestones: []Milestone{}, Evidence: []Evidence{}}
		return nil
	}))
	return st
}

func taskJSON(t *testing.T, st *Store, id string) string {
	t.Helper()
	s, e := st.read()
	must(t, e)
	b, e := json.Marshal(s.Tasks[id])
	must(t, e)
	return string(b)
}

func ledgerJSON(t *testing.T, st *Store) string {
	t.Helper()
	var raw string
	must(t, st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw))
	return raw
}

func TestBriefNativeClaudeAndUnchangedLedger(t *testing.T) {
	for _, phase := range []TaskPhase{TaskPhaseInProgress, TaskPhaseInReview, TaskPhaseDone} {
		t.Run(string(phase), func(t *testing.T) {
			st := briefStore(t, phase)
			sock := filepath.Join(t.TempDir(), "native.sock")
			l, err := net.Listen("unix", sock)
			must(t, err)
			defer l.Close()
			must(t, st.update(func(s *State) error { m := s.Members["master"]; m.Peer = sock; m.EngineID = "native"; return nil }))
			briefMasterPane(t, st)
			briefAttachedComposer(t, st)
			before := ledgerJSON(t, st)
			frames := make(chan map[string]any, 2)
			go func() {
				for i := 0; i < 2; i++ {
					c, e := l.Accept()
					if e != nil {
						return
					}
					var frame map[string]any
					_ = json.NewDecoder(c).Decode(&frame)
					_ = c.Close()
					frames <- frame
				}
			}()
			want, err := prompts.Brief("T1")
			must(t, err)
			for i := 0; i < 2; i++ {
				note, e := briefRequest(st, "master", "T1")
				must(t, e)
				if !strings.Contains(note, "native input") {
					t.Fatal(note)
				}
				select {
				case frame := <-frames:
					expected := map[string]any{"type": "user", "session_id": "native", "priority": "next", "message": map[string]any{"role": "user", "content": want}}
					if !reflect.DeepEqual(frame, expected) {
						t.Fatalf("unexpected native frame: %#v", frame)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("no native delivery")
				}
			}
			if after := ledgerJSON(t, st); after != before {
				t.Fatal("brief wrote the ledger")
			}
		})
	}
}

func TestBriefNativeCodexCustomCommandAndAlias(t *testing.T) {
	for _, shell := range []string{"", "bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if shell != "" {
				if _, e := exec.LookPath(shell); e != nil {
					t.Skip(e)
				}
			}
			st := briefStore(t, TaskPhaseDone)
			root := t.TempDir()
			capture := filepath.Join(root, "args")
			bin := filepath.Join(root, "native")
			must(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$CAPTURE\"\n"), 0700))
			cfg := config.Defaults()
			cmd := config.Command{Executable: bin, Args: []string{"literal prefix"}}
			env := map[string]string{"CAPTURE": capture, "HOME": root, "ZDOTDIR": root}
			if shell != "" {
				must(t, os.WriteFile(filepath.Join(root, ".bashrc"), []byte("alias briefnative="+shellQuote(bin)+"\n"), 0600))
				must(t, os.WriteFile(filepath.Join(root, ".zshrc"), []byte("alias briefnative="+shellQuote(bin)+"\n"), 0600))
				cmd.Executable = "briefnative"
				cmd.Shell = shell
			}
			cfg.EngineCommands = map[config.Engine]config.Command{config.Codex: cmd}
			must(t, st.update(func(s *State) error {
				s.Config = &cfg
				m := s.Members["master"]
				m.Engine = config.Codex
				m.EngineID = "thread-7"
				m.Cwd = root
				m.Env = env
				return nil
			}))
			briefMasterPane(t, st)
			briefAttachedComposer(t, st)
			before := ledgerJSON(t, st)
			for i := 0; i < 2; i++ {
				_, e := briefRequest(st, "master", "T1")
				must(t, e)
			}
			raw, e := os.ReadFile(capture)
			must(t, e)
			text, e := prompts.Brief("T1")
			must(t, e)
			want := strings.Repeat("literal prefix\nqueue\n--thread\nthread-7\n--message\n"+text+"\n", 2)
			if string(raw) != want {
				t.Fatalf("native args: %q", raw)
			}
			if ledgerJSON(t, st) != before {
				t.Fatal("native queue wrote ledger")
			}
			// The helper fails visibly; there is no durable retry on a later sync.
			must(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 9\n"), 0700))
			if _, e = briefRequest(st, "master", "T1"); e == nil {
				t.Fatal("helper failure hidden")
			}
			must(t, st.syncMessages())
			if ledgerJSON(t, st) != before {
				t.Fatal("failed request wrote ledger")
			}
		})
	}
}

func TestBriefUnavailableAndStaleRequests(t *testing.T) {
	for _, mode := range []string{"stopped", "removed", "stopping", "needs_attention", "crashed", "unregistered", "socket-failure", "missing-peer", "stale", "inactive", "worker", "missing-task"} {
		t.Run(mode, func(t *testing.T) {
			st := briefStore(t, TaskPhaseDone)
			actor, id := "master", "T1"
			must(t, st.update(func(s *State) error {
				m := s.Members["master"]
				m.EngineID = "native"
				m.Peer = filepath.Join(t.TempDir(), "absent.sock")
				switch mode {
				case "stopped", "removed", "stopping", "needs_attention", "crashed":
					m.State = MemberState(mode)
				case "unregistered":
					m.EngineID = ""
				case "missing-peer":
					m.Peer = ""
				case "inactive":
					s.Active = false
				case "worker":
					actor = "a"
				case "missing-task":
					id = "T404"
				}
				return nil
			}))
			if mode == "socket-failure" || mode == "missing-peer" {
				briefMasterPane(t, st)
			}
			if mode == "stale" {
				st.Actor = "master"
				st.Generation = 9
			}
			before := ledgerJSON(t, st)
			_, requestErr := briefRequest(st, actor, id)
			if requestErr == nil {
				t.Fatal("unavailable request accepted")
			}
			if mode == "socket-failure" && !strings.Contains(requestErr.Error(), "brief transport failed") {
				t.Fatalf("did not reach socket transport: %v", requestErr)
			}
			if mode == "missing-peer" && !strings.Contains(requestErr.Error(), "master native inbox is not ready") {
				t.Fatalf("did not reach inbox check: %v", requestErr)
			}
			if before != ledgerJSON(t, st) {
				t.Fatal("refusal wrote ledger")
			}
		})
	}
}

func TestLegacyBriefIsAuditOnly(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	var id string
	must(t, st.update(func(s *State) error {
		m := s.message(UserSender, "master", "T1", "old Brief", "")
		m.RequestKey = UserSender + ":brief:T1"
		id = m.ID
		return nil
	}))
	before := ledgerJSON(t, st)
	must(t, st.syncMessages())
	must(t, st.deliver(id))
	if err := messageCommand(st, "master", []string{"message", "retry", id}, options{}); err == nil {
		t.Fatal("legacy retry accepted")
	}
	if before != ledgerJSON(t, st) {
		t.Fatal("legacy history changed")
	}
	s, e := st.read()
	must(t, e)
	s.Messages[0].recoverDelivery()
	if s.Messages[0].Attempts != 0 || s.Messages[0].State != DeliveryStatePending {
		t.Fatal("legacy recovery changed history")
	}
}

func briefMasterPane(t *testing.T, st *Store) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip(err)
	}
	socket := filepath.Join(t.TempDir(), "isolated.sock")
	pane, err := exec.Command("tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "brief-test", "-x", "100", "-y", "30", "-P", "-F", "#{pane_id}", "cat").Output()
	must(t, err)
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	must(t, st.update(func(s *State) error {
		s.Socket = socket
		s.Members["master"].Pane = strings.TrimSpace(string(pane))
		s.Members["master"].Session = "brief-test"
		return nil
	}))
}

// Keep a real client attached with unfinished input while native transport runs.
func briefAttachedComposer(t *testing.T, st *Store) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip(err)
	}
	s, err := st.read()
	must(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	hold := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "hold", s.Socket, "brief-test", "10")
	stdout, err := hold.StdoutPipe()
	must(t, err)
	must(t, hold.Start())
	t.Cleanup(func() { cancel(); _ = hold.Wait() })
	buf := make([]byte, len("attached\n"))
	_, err = io.ReadFull(stdout, buf)
	must(t, err)
	_, err = tm(s, "send-keys", "-t", s.Members["master"].Pane, "-l", "--", "unfinished user draft")
	must(t, err)
	// Capture once after the typed draft is observable; do not submit it.
	var before string
	for i := 0; i < 100; i++ {
		before, err = tm(s, "capture-pane", "-p", "-t", s.Members["master"].Pane)
		must(t, err)
		if strings.Contains(before, "unfinished user draft") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(before, "unfinished user draft") {
		t.Fatal("draft never appeared")
	}
	t.Cleanup(func() {
		after, e := tm(s, "capture-pane", "-p", "-t", s.Members["master"].Pane)
		if e != nil || after != before {
			t.Errorf("native Brief changed terminal input: %v before=%q after=%q", e, before, after)
		}
	})
}

func TestBriefDoneTaskCommandBypassesPhaseGuard(t *testing.T) {
	st := briefStore(t, TaskPhaseDone)
	must(t, st.update(func(s *State) error { s.Members["master"].State = MemberStateStopped; return nil }))
	err := taskCommand(st, "master", []string{"brief", "T1"}, options{})
	if err == nil || !strings.Contains(err.Error(), "no master session") {
		t.Fatalf("done task Brief blocked by phase: %v", err)
	}
}
