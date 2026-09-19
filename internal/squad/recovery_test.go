package squad

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/process"
)

func TestCrashCleanupAndProjectResume(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	temp, e := os.MkdirTemp("", "csq-recover-test-")
	must(t, e)
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "csquad")
	out, e := exec.Command("go", "build", "-o", binary, "../../cmd/csquad").CombinedOutput()
	if e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	fake := filepath.Join(temp, "engines")
	must(t, os.Mkdir(fake, 0700))
	must(t, os.WriteFile(filepath.Join(fake, "claude"), []byte("#!/bin/sh\nif [ \"$1\" = agents ]; then echo '[]'; exit 0; fi\nsleep 300 &\nwait\n"), 0700))
	root := filepath.Join(temp, "repo")
	must(t, os.Mkdir(root, 0700))
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		_, e = git(root, args...)
		must(t, e)
	}
	socket := filepath.Join(temp, "s")
	_, e = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "unrelated", "sleep", "300")
	must(t, e)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	env := agentenv.Environ(map[string]string{"PATH": fake + ":" + os.Getenv("PATH"), "CSQUAD_CONFIG": filepath.Join(temp, "config.toml"), "CSQUAD_HOME": "", "CSQUAD_STATE_DIR": "", "CSQUAD_MEMBER_ID": "", "CSQUAD_GENERATION": "", "TMUX": socket + ",0,0"})
	cli := func(args ...string) []byte {
		t.Helper()
		c := exec.Command(binary, args...)
		c.Dir = root
		c.Env = env
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %v %s", args, e, out)
		}
		return out
	}
	cli("start", "--name", "test", "--engine", "claude", "--detach")
	dir := filepath.Join(root, ".csquad", "teams", "test")
	st, e := openStore(dir)
	must(t, e)
	defer st.DB.Close()
	defer func() {
		s, e := st.read()
		if e == nil {
			s.Executable = binary
			stop(st)
		}
	}()
	read := func() *State { t.Helper(); s, e := st.read(); must(t, e); return s }
	wait := func(fn func(*State) bool) *State {
		t.Helper()
		until := time.Now().Add(20 * time.Second)
		for time.Now().Before(until) {
			s := read()
			if fn(s) {
				return s
			}
			time.Sleep(100 * time.Millisecond)
		}
		s := read()
		b, _ := json.Marshal(s)
		t.Fatalf("timeout: %s", b)
		return nil
	}
	cli("member", "add", "alice", "--engine", "claude", "--role", "custom role", "--instructions", "preserve my work")
	cli("task", "create", "code", "--code", "--acceptance", "preserve edits")
	s := read()
	var taskID, workspace string
	for id, task := range s.Tasks {
		taskID = id
		workspace = task.Workspace
	}
	cli("task", "assign", taskID, "--owner", "alice")
	must(t, os.WriteFile(filepath.Join(workspace, "uncommitted.txt"), []byte("keep me"), 0600))
	wait(func(s *State) bool { return s.Members["master"].EnginePID > 0 && s.Members["alice"].EnginePID > 0 })
	// Controlled restart emits old pane exit hooks; it must not stop the team.
	old := read()
	cli("member", "restart", "master")
	wait(func(s *State) bool {
		return s.Active && s.Members["master"].Generation > old.Members["master"].Generation && s.Members["master"].EnginePID > 0
	})
	time.Sleep(400 * time.Millisecond)
	if !read().Active {
		t.Fatal("controlled restart stopped the team")
	}
	// Capture descendant identities, then kill the wrapper rather than the engine.
	must(t, st.refresh())
	before := read()
	must(t, syscall.Kill(before.Members["master"].RunnerPID, syscall.SIGKILL))
	wait(func(s *State) bool { return !s.Active && s.Phase == TeamPhaseInterrupted })
	all, e := process.Snapshot()
	must(t, e)
	for _, m := range before.Members {
		for _, p := range m.Processes {
			if process.Alive(p, all) {
				t.Fatalf("orphan process %d", p.PID)
			}
		}
	}
	if _, e = os.Stat(filepath.Join(dir, "runtime")); !os.IsNotExist(e) {
		t.Fatal("runtime not cleaned")
	}
	_, e = process.Run("", "tmux", "-S", socket, "has-session", "-t", "=unrelated")
	must(t, e)
	sessions, e := process.Run("", "tmux", "-S", socket, "list-sessions", "-F", "#{session_name}")
	must(t, e)
	if strings.Contains(sessions, "csq-test-") {
		t.Fatal("team session remains", sessions)
	}
	if out := string(cli("list")); !strings.Contains(out, "test") || !strings.Contains(out, "stopped") {
		t.Fatal("stopped team missing from list:", out)
	}
	cli("start", "--name", "test", "--detach")
	after := wait(func(s *State) bool {
		return s.Active && s.Phase == TeamPhaseRunning && s.Members["alice"].EnginePID > 0
	})
	if after.Epoch <= before.Epoch || after.Tasks[taskID].Workspace != workspace || after.Tasks[taskID].Owner != "alice" || after.Members["alice"].Instructions != "preserve my work" {
		t.Fatal("recovery lost state")
	}
	b, e := os.ReadFile(filepath.Join(workspace, "uncommitted.txt"))
	must(t, e)
	if string(b) != "keep me" {
		t.Fatal("work lost")
	}
	epoch := read().Epoch
	cli("start", "--name", "test", "--detach")
	if read().Epoch != epoch {
		t.Fatal("start restarted a running team")
	}
	// A delayed old shutdown must not kill a resumed team.
	cli("shutdown", "--epoch", strconv.Itoa(before.Epoch), "--expected-generation", strconv.Itoa(before.Members["master"].Generation), "--reason", "master_exit")
	if !read().Active {
		t.Fatal("old shutdown killed recovered team")
	}
	status, e := git(root, "status", "--porcelain")
	must(t, e)
	if strings.Contains(status, ".csquad") {
		t.Fatal("state pollutes Git status")
	}
	// Even loss of the entire tmux server is recoverable on next invocation.
	must(t, st.refresh())
	lost := read()
	pid, e := tm(lost, "display-message", "-p", "#{pid}")
	must(t, e)
	serverPID, e := strconv.Atoi(pid)
	must(t, e)
	must(t, syscall.Kill(serverPID, syscall.SIGKILL))
	time.Sleep(150 * time.Millisecond)
	cli("resume", "--detach")
	wait(func(s *State) bool { return s.Active && s.Phase == TeamPhaseRunning && s.Epoch > lost.Epoch })
	all, e = process.Snapshot()
	must(t, e)
	for _, m := range lost.Members {
		for _, p := range m.Processes {
			if process.Alive(p, all) {
				t.Fatalf("server crash orphan %d", p.PID)
			}
		}
	}
	cli("stop")
	if read().Phase != TeamPhaseStopped {
		t.Fatal("normal stop not recorded")
	}
}

func TestCleanupRefusesOtherTeamSession(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	dir, e := os.MkdirTemp("", "csq-owner-test-")
	must(t, e)
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	_, e = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "same-name", "sleep", "60")
	must(t, e)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	st := testStore(t)
	must(t, st.update(func(s *State) error { s.Socket = socket; s.Members["a"].Session = "same-name"; return nil }))
	s, _ := st.read()
	_, e = tm(s, "set-option", "-t", "=same-name", "@csquad_team", "another-project")
	must(t, e)
	if e = killMember(st, "a"); e == nil {
		t.Fatal("accepted foreign session")
	}
	_, e = tm(s, "has-session", "-t", "=same-name")
	must(t, e)
}
