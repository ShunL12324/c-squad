package squad

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/process"
)

func TestSavedTeamRemovalStaleRealTmuxSocket(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	st, _ := savedRemovalTeam(t, false)
	// Keep the Unix socket below the platform path-length limit.
	dir, err := os.MkdirTemp("", "csq-remove-socket-")
	must(t, err)
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "tmux.sock")
	_, err = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "removal-live", "sleep", "300")
	must(t, err)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	must(t, st.update(func(s *State) error {
		s.Socket = socket
		s.Members["master"] = &Member{ID: "master", Session: "removal-live"}
		return nil
	}))
	if _, err = removeTeamDirectory(st.Dir, true); err == nil || !strings.Contains(err.Error(), "member session") {
		t.Fatalf("live member session accepted: %v", err)
	}
	s, err := st.read()
	must(t, err)
	_, err = tm(s, "rename-session", "-t", "removal-live", runtimeName(s))
	must(t, err)
	if _, err = removeTeamDirectory(st.Dir, true); err == nil || !strings.Contains(err.Error(), "runtime session") {
		t.Fatalf("live runtime session accepted: %v", err)
	}
	pidText, err := tm(s, "display-message", "-p", "#{pid}")
	must(t, err)
	pid, err := strconv.Atoi(pidText)
	must(t, err)
	must(t, syscall.Kill(pid, syscall.SIGKILL))
	deadline := time.Now().Add(3 * time.Second)
	for {
		absent, e := removalSocketAbsent(socket)
		if e == nil && absent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("killed server still reachable: absent=%v error=%v", absent, e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := os.Stat(socket)
	must(t, err)
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("fixture did not leave a stale socket")
	}
	// The original tmux command fails on exactly this stale endpoint.
	if _, err = tm(s, "list-sessions", "-F", "#{session_name}"); err == nil {
		t.Fatal("stale server unexpectedly listed sessions")
	}
	before, err := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, err)
	plan, err := removeTeamDirectory(st.Dir, true)
	must(t, err)
	if !plan.DryRun || plan.Removed {
		t.Fatalf("unexpected dry-run result: %+v", plan)
	}
	after, err := os.ReadFile(filepath.Join(st.Dir, "state.db"))
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run changed saved ledger")
	}
	if _, err = os.Stat(socket); err != nil {
		t.Fatal("inspection removed stale socket", err)
	}
	must(t, st.update(func(s *State) error { s.Members["master"].RunnerPID = os.Getpid(); return nil }))
	if _, err = removeTeamDirectory(st.Dir, true); err == nil || !strings.Contains(err.Error(), "live process") {
		t.Fatalf("stale socket bypassed process guard: %v", err)
	}
}

func TestSavedTeamRemovalSocketInspectionFailsClosed(t *testing.T) {
	for _, mode := range []string{"regular-file", "permission-path", "protocol", "permission-report", "unknown-report"} {
		t.Run(mode, func(t *testing.T) {
			st, _ := savedRemovalTeam(t, false)
			dir, err := os.MkdirTemp("", "csq-remove-error-")
			must(t, err)
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "tmux.sock")
			if mode == "regular-file" || mode == "permission-path" {
				must(t, os.WriteFile(socket, []byte("not a socket"), 0600))
				if mode == "permission-path" {
					if os.Geteuid() == 0 {
						t.Skip("root bypasses filesystem permission checks")
					}
					must(t, os.Chmod(dir, 0))
					defer os.Chmod(dir, 0700)
				}
			} else {
				listener, err := net.Listen("unix", socket)
				must(t, err)
				defer listener.Close()
				// A reachable endpoint does not imply a valid tmux server. Close every
				// accepted connection so the actual tmux client reports a protocol error.
				go func() {
					for {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						_ = conn.Close()
					}
				}()
				if mode != "protocol" {
					fake := filepath.Join(dir, "bin")
					must(t, os.Mkdir(fake, 0700))
					message := map[string]string{"permission-report": "permission denied", "unknown-report": "unknown inspection failure"}[mode]
					must(t, os.WriteFile(filepath.Join(fake, "tmux"), []byte("#!/bin/sh\necho '"+message+"' >&2\nexit 1\n"), 0700))
					t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
			}
			must(t, st.update(func(s *State) error { s.Socket = socket; return nil }))
			if _, err := removeTeamDirectory(st.Dir, true); err == nil || !strings.Contains(err.Error(), "cannot verify remaining tmux sessions") {
				t.Fatalf("unverifiable endpoint accepted: %v", err)
			}
			if _, err := os.Stat(filepath.Join(st.Dir, "state.db")); err != nil {
				t.Fatal("ledger removed on failed inspection", err)
			}
		})
	}
}
