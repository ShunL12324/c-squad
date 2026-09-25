package squad

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	// Only the fake claude exists, so both pointers select a profile launching it.
	configPath := filepath.Join(temp, "config.toml")
	must(t, os.WriteFile(configPath, []byte("version = 2\ndefault_profile = \"only\"\nmaster_profile = \"only\"\n[profiles.only]\nengine = \"claude\"\n"), 0600))
	env := agentenv.Environ(map[string]string{"PATH": fake + ":" + os.Getenv("PATH"), "CSQUAD_CONFIG": configPath, "CSQUAD_HOME": "", "CSQUAD_STATE_DIR": "", "CSQUAD_MEMBER_ID": "", "CSQUAD_GENERATION": "", "TMUX": socket + ",0,0"})
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
	// Failure to reap an older corrupt ledger must not reserve a new name.
	broken := filepath.Join(root, ".csquad", "teams", "old")
	must(t, os.MkdirAll(broken, 0700))
	must(t, os.WriteFile(filepath.Join(broken, "state.db"), []byte("invalid sqlite database"), 0600))
	failedStart := exec.Command(binary, "start", "test", "--detach")
	failedStart.Dir, failedStart.Env = root, env
	if out, err := failedStart.CombinedOutput(); err == nil {
		t.Fatalf("corrupt old ledger should block cleanup: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".csquad", "teams", "test", "state.db")); !os.IsNotExist(err) {
		t.Fatalf("cleanup failure reserved new team name: %v", err)
	}
	must(t, os.RemoveAll(broken))
	// Retrying after removing that corruption must succeed, even with competition.
	// Two creators of the same name must produce exactly one live team.
	type creation struct {
		out []byte
		err error
	}
	results := make(chan creation, 2)
	for range 2 {
		go func() {
			c := exec.Command(binary, "start", "test", "--detach")
			c.Dir, c.Env = root, env
			out, err := c.CombinedOutput()
			results <- creation{out, err}
		}()
	}
	successes := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
		} else if !strings.Contains(string(result.out), "already exists") {
			t.Fatalf("concurrent create: %v %s", result.err, result.out)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent creators succeeded %d times", successes)
	}
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
	project := filepath.Join(temp, "member project")
	must(t, os.Mkdir(project, 0700))
	cli("member", "add", "alice", "--profile", "only", "--instructions", "preserve my work", "--cwd", project)
	wait(func(s *State) bool { return s.Members["alice"].EnginePID > 0 })
	assertDirectory := func(want string) {
		t.Helper()
		s := read()
		got, e := tm(s, "display-message", "-p", "-t", s.Members["alice"].Pane, "#{pane_current_path}")
		must(t, e)
		if got != want || s.Members["alice"].Cwd != want {
			t.Fatalf("member directory: process=%q ledger=%q want=%q", got, s.Members["alice"].Cwd, want)
		}
	}
	assertDirectory(project)
	cli("member", "restart", "alice", "--cwd", root)
	wait(func(s *State) bool { return s.Members["alice"].EnginePID > 0 })
	assertDirectory(root)

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
	// A real successful transport is durable independently of an optional ACK.
	inbox, e := net.Listen("unix", filepath.Join(temp, "delivered.sock"))
	must(t, e)
	defer inbox.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, err := inbox.Accept()
		if err != nil {
			accepted <- err
			return
		}
		defer conn.Close()
		var frame map[string]any
		accepted <- json.NewDecoder(conn).Decode(&frame)
	}()
	var deliveredID string
	actAsPinnedBuild(t, st)
	must(t, st.update(func(s *State) error {
		s.Members["master"].Peer = inbox.Addr().String()
		s.Members["master"].EngineID = "delivered-session"
		deliveredID = s.message("alice", "master", "", "transport accepted; no ACK required", "").ID
		return nil
	}))
	must(t, st.deliver(deliveredID))
	select {
	case err := <-accepted:
		must(t, err)
	case <-time.After(time.Second):
		t.Fatal("message did not reach native peer")
	}
	assertNotRequeued := func() {
		t.Helper()
		for _, msg := range read().Messages {
			if msg.ID == deliveredID {
				if msg.State != DeliveryStateSent || msg.Attempts != 1 || msg.Error != "" {
					t.Fatalf("successful unacknowledged message requeued: %+v", msg)
				}
				return
			}
		}
		t.Fatal("delivered message disappeared")
	}
	assertNotRequeued()
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
	assertNotRequeued()
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
	for _, args := range [][]string{{"start", "test", "--detach"}, {"--name", "test", "--detach"}} {
		c := exec.Command(binary, args...)
		c.Dir, c.Env = root, env
		prior := read()
		if out, err := c.CombinedOutput(); err == nil || !strings.Contains(string(out), "already exists") {
			t.Fatalf("collision %v: %v %s", args, err, out)
		}
		next := read()
		if next.Epoch != prior.Epoch || next.Active != prior.Active || next.Phase != prior.Phase {
			t.Fatal("rejected start resumed interrupted team")
		}
	}
	cli("resume", "test", "--detach")
	after := wait(func(s *State) bool {
		return s.Active && s.Phase == TeamPhaseRunning && s.Members["alice"].EnginePID > 0
	})
	assertNotRequeued()
	if after.Epoch <= before.Epoch || after.Tasks[taskID].Workspace != workspace || after.Tasks[taskID].Owner != "alice" || after.Members["alice"].Instructions != "preserve my work" {
		t.Fatal("recovery lost state")
	}
	b, e := os.ReadFile(filepath.Join(workspace, "uncommitted.txt"))
	must(t, e)
	if string(b) != "keep me" {
		t.Fatal("work lost")
	}
	epoch := read().Epoch
	c := exec.Command(binary, "start", "test", "--detach")
	c.Dir, c.Env = root, env
	if out, err := c.CombinedOutput(); err == nil || !strings.Contains(string(out), "already exists") {
		t.Fatalf("start collision: %v %s", err, out)
	}
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

// Issue #27: run-shell expands tmux formats, so a project path containing
// "#S" pointed shutdown at the wrong team directory and Master's exit never
// closed the team. Both the direct request and the pane-died hook must pass
// the exact directory through.
func TestShutdownCommandSurvivesFormatCharactersInPaths(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	tmp, e := os.MkdirTemp("", "csq-fmt-")
	must(t, e)
	defer os.RemoveAll(tmp)
	root := filepath.Join(tmp, "proj #S #W ##")
	st, e := openStore(filepath.Join(root, "state"))
	must(t, e)
	defer st.DB.Close()
	capture := filepath.Join(tmp, "capture")
	fake := filepath.Join(root, "csquad #{session_name}")
	must(t, os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$0.$$\"\nmv \"$0.$$\" "+shellQuote(capture)+"-$(date +%s%N)\n"), 0700))
	socket := filepath.Join(tmp, "s")
	// tmux executes a shell command. Replace that shell with sleep so pane_pid
	// names the process whose exit must emit pane-died.
	_, e = process.Run(tmp, "tmux", "-vv", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "team-master", "exec sleep 300")
	must(t, e)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	must(t, st.update(func(s *State) error {
		*s = State{Version: 1, ID: "fmt", Root: root, Active: true, Socket: socket, Executable: fake, Members: map[string]*Member{"master": {ID: "master", Session: "team-master", Generation: 1}}}
		return nil
	}))
	s, e := st.read()
	must(t, e)
	pane, e := tm(s, "display-message", "-p", "-t", "=team-master:", "#{pane_id}")
	must(t, e)
	_, e = tm(s, "set-window-option", "-t", "=team-master:", "remain-on-exit", "on")
	must(t, e)
	must(t, st.update(func(s *State) error { s.Members["master"].Pane = pane; return nil }))
	must(t, installMasterHook(st))
	must(t, requestShutdown(st, s, "reason #S;"))
	// Trigger the exit only after the hook is installed; a fixed short sleep
	// can expire during setup on a loaded CI runner.
	pidText, e := tm(s, "display-message", "-p", "-t", pane, "#{pane_pid}")
	must(t, e)
	pid, e := strconv.Atoi(pidText)
	must(t, e)
	prePaneProc := linuxProcStatus(pid)
	serverPIDText, serverPIDErr := tm(s, "display-message", "-p", "-t", pane, "#{pid}")
	if serverPIDErr != nil {
		serverPIDText = serverPIDErr.Error()
	}
	serverPID, _ := strconv.Atoi(serverPIDText)
	preServerProc := linuxProcStatus(serverPID)
	t.Logf("pre-signal tmux server PID=%q; pane /proc status=%v; server /proc status=%v", serverPIDText, prePaneProc, preServerProc)
	preSignal, preErr := tm(s, "display-message", "-p", "-t", pane, "#{pane_dead}|#{pane_pid}")
	if preErr != nil {
		preSignal = preErr.Error()
	}
	preProcess, preProcessErr := process.Run("", "ps", "-p", pidText, "-o", "pid=,ppid=,stat=,comm=")
	if preProcessErr != nil {
		preProcess = preProcessErr.Error()
	}
	preRemain, preRemainErr := tm(s, "show-options", "-wv", "-t", "=team-master:", "remain-on-exit")
	if preRemainErr != nil {
		preRemain = preRemainErr.Error()
	}
	proc, e := os.FindProcess(pid)
	must(t, e)
	must(t, proc.Signal(syscall.SIGTERM))
	// Observe the actual pane death before judging hook delivery. Otherwise a
	// surviving shell/child process looks like a missing pane-died callback.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		dead, err := tm(s, "display-message", "-p", "-t", pane, "#{pane_dead}")
		must(t, err)
		if dead == "1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("signaled pane PID %d but pane did not exit", pid)
		}
	}
	want := map[string]bool{"request": false, "master_exit": false}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		files, _ := filepath.Glob(capture + "-*")
		for _, file := range files {
			b, _ := os.ReadFile(file)
			args := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			if len(args) < 2 || args[0] != "--team" || args[1] != st.Dir {
				t.Fatalf("shutdown argv = %q, want --team %q", args, st.Dir)
			}
			switch args[len(args)-1] {
			case "reason #S;":
				want["request"] = true
			case "master_exit":
				want["master_exit"] = true
			default:
				t.Fatalf("reason changed: %q", args)
			}
		}
		if want["request"] && want["master_exit"] {
			return
		}
		if time.Now().After(deadline) {
			inspect := func(args ...string) string {
				out, err := tm(s, args...)
				if err != nil {
					return fmt.Sprintf("%v: %v", args, err)
				}
				return out
			}
			partial, _ := filepath.Glob(fake + ".*")
			serverEvents, serverLog := tmuxPaneExitLog(tmp, pane)
			t.Fatalf("shutdown not requested with the exact path: %v; pre_signal=%q; pre_process=%q; pre_remain=%q; pre_server_pid=%q; pre_pane_proc=%v; pre_server_proc=%v; pane_proc=%v; server_proc=%v; hooks=%q; pane_state=%q; pane=%q; server_events=%q; server_log=%q; captures=%v; partial=%v",
				want,
				preSignal, preProcess, preRemain,
				serverPIDText,
				prePaneProc, preServerProc, linuxProcStatus(pid), linuxProcStatus(serverPID),
				inspect("show-hooks", "-w", "-t", "=team-master:"),
				inspect("display-message", "-p", "-t", pane, "#{pane_dead}|#{pane_dead_status}|#{pane_dead_signal}|#{pane_dead_time}|#{pane_pid}"),
				inspect("capture-pane", "-p", "-t", pane),
				serverEvents, serverLog, files, partial)
		}
	}
}

// Read only the process and signal fields needed to diagnose a missing
// SIGCHLD. A nil result means the process is gone or /proc is unavailable.
func linuxProcStatus(pid int) map[string]string {
	if runtime.GOOS != "linux" || pid <= 0 {
		return nil
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch key {
		case "Pid", "PPid", "State", "SigBlk", "SigIgn", "SigCgt", "SigPnd", "ShdPnd":
			fields[key] = strings.TrimSpace(value)
		}
	}
	return fields
}

// Report bounded child-exit events separately from hook-dispatch lines so
// later hook output cannot evict the signal/reap evidence. The full -vv log
// can contain unrelated terminal data.
func tmuxPaneExitLog(dir, pane string) (events, hooks []string) {
	logs, _ := filepath.Glob(filepath.Join(dir, "tmux-server-*.log"))
	const maxEvents, maxHooks, maxLineLen = 40, 40, 400
	for _, name := range logs {
		data, err := os.ReadFile(name)
		if err != nil {
			events = append(events, fmt.Sprintf("%s: %v", filepath.Base(name), err))
			if len(events) > maxEvents {
				events = events[1:]
			}
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			childEvent := strings.Contains(line, "server_signal:") ||
				strings.Contains(line, "job died ") ||
				strings.Contains(line, pane+" exited") ||
				strings.Contains(line, pane+" error")
			hookEvent := strings.Contains(line, "pane-died") ||
				strings.Contains(line, "notify_insert_hook") ||
				strings.Contains(line, "notify_callback")
			if !childEvent && !hookEvent {
				continue
			}
			if len(line) > maxLineLen {
				line = line[:maxLineLen] + "..."
			}
			if childEvent {
				events = append(events, line)
				if len(events) > maxEvents {
					events = events[1:]
				}
			}
			if hookEvent {
				hooks = append(hooks, line)
				if len(hooks) > maxHooks {
					hooks = hooks[1:]
				}
			}
		}
	}
	if len(logs) == 0 {
		return []string{"tmux server log unavailable"}, nil
	}
	return events, hooks
}
