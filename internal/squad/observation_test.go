package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func TestObserveSharesClaudeListingWithinAccount(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "tmux.sock")
	_, err := process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "keep", "sleep", "60")
	must(t, err)
	t.Cleanup(func() { _, _ = process.Run("", "tmux", "-S", socket, "kill-server") })
	log := filepath.Join(dir, "calls")
	helper := filepath.Join(dir, "claude-helper")
	must(t, os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$ACCOUNT\" >> \"$CALL_LOG\"\nprintf '%s\\n' '[{\"sessionId\":\"s-master\",\"status\":\"idle\"},{\"sessionId\":\"s-a\",\"status\":\"busy\"},{\"sessionId\":\"s-b\",\"status\":\"idle\"}]'\n"), 0700))
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Profiles = map[string]config.Profile{"one": {Engine: config.Claude, Command: &config.Command{Executable: helper}}}
		s.Config, s.Socket = &cfg, socket
		for id, m := range s.Members {
			m.Profile, m.EngineID, m.Session = "one", "s-"+id, "obs-"+id
			m.Env = map[string]string{"ACCOUNT": "shared", "CALL_LOG": log}
		}
		s.Members["master"].Env["ACCOUNT"] = "other"
		return nil
	}))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Members {
		_, err = tm(s, "new-session", "-d", "-s", m.Session, "sleep", "60")
		must(t, err)
	}
	for pass := 1; pass <= 2; pass++ {
		known, err := st.observe()
		must(t, err)
		if !known["master"] || !known["a"] || !known["b"] {
			t.Fatalf("pass %d known = %v", pass, known)
		}
		calls, err := os.ReadFile(log)
		must(t, err)
		if got := strings.Count(string(calls), "shared\n"); got != pass {
			t.Fatalf("shared account called %d times after %d passes", got, pass)
		}
		if got := strings.Count(string(calls), "other\n"); got != pass {
			t.Fatalf("separate account called %d times after %d passes", got, pass)
		}
	}
	s, err = st.read()
	must(t, err)
	if s.Members["a"].State != MemberStateWorking || s.Members["b"].State != MemberStateIdle {
		t.Fatalf("members did not receive their own session status: a=%s b=%s", s.Members["a"].State, s.Members["b"].State)
	}
}

func TestBoardAndMemberListUseStoredObservation(t *testing.T) {
	st := testStore(t)
	marker := filepath.Join(t.TempDir(), "probed")
	helper := filepath.Join(t.TempDir(), "helper")
	must(t, os.WriteFile(helper, []byte("#!/bin/sh\ntouch \"$PROBE_MARKER\"\nprintf '[]\\n'\n"), 0700))
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Profiles = map[string]config.Profile{"probe": {Engine: config.Claude, Command: &config.Command{Executable: helper}}}
		s.Config = &cfg
		for _, m := range s.Members {
			m.Profile = "probe"
			m.Env = map[string]string{"PROBE_MARKER": marker}
		}
		return nil
	}))
	// These reads must not launch a native helper, even when no runtime has
	// observed the team yet. The stored timestamp communicates that age.
	must(t, Execute([]string{"board"}, options{"team": st.Dir}, nil))
	must(t, memberCommand(st, "master", []string{"list"}, options{}))
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("read launched a helper: %v", err)
	}
}
