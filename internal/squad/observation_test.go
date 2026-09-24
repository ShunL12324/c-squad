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

func TestObserveSharesClaudeListingWithinAccount(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "tmux.sock")
	_, err := process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "keep", "sleep", "60")
	must(t, err)
	t.Cleanup(func() { _, _ = process.Run("", "tmux", "-S", socket, "kill-server") })
	log := filepath.Join(dir, "calls")
	helper := filepath.Join(dir, "claude-helper")
	must(t, os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$ACCOUNT\" >> \"$CALL_LOG\"\nprintf '%s\\n' '[{\"sessionId\":\"s-master\",\"status\":\"idle\"},{\"sessionId\":\"s-a\",\"status\":\"busy\"},{\"sessionId\":\"s-b\",\"status\":\"idle\"}]'\n"), 0700))
	must(t, os.Symlink(helper, filepath.Join(dir, "helperalias")))
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Profiles = map[string]config.Profile{"one": {Engine: config.Claude, Command: &config.Command{Executable: helper}}}
		s.Config, s.Socket = &cfg, socket
		for id, m := range s.Members {
			m.Profile, m.EngineID, m.Session = "one", "s-"+id, "obs-"+id
			m.Cwd = filepath.Join(dir, "cwd-"+id)
			m.Env = map[string]string{"ACCOUNT": "shared", "CALL_LOG": log, "PATH": dir + ":/usr/bin:/bin"}
		}
		s.Members["master"].Env["ACCOUNT"] = "other"
		return nil
	}))
	s, err := st.read()
	must(t, err)
	for _, m := range s.Members {
		must(t, os.Mkdir(m.Cwd, 0700))
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
	// Interactive shell startup may inspect each worktree, so the same account
	// must be probed separately when the configured command uses shell mode.
	must(t, os.Remove(log))
	must(t, st.update(func(s *State) error {
		cfg := *s.Config
		cfg.Profiles = map[string]config.Profile{"one": {Engine: config.Claude, Command: &config.Command{Shell: "bash", Executable: "helperalias"}}}
		s.Config = &cfg
		return nil
	}))
	known, err := st.observe()
	must(t, err)
	if !known["master"] || !known["a"] || !known["b"] {
		t.Fatalf("shell mode known = %v", known)
	}
	calls, err := os.ReadFile(log)
	must(t, err)
	if got := strings.Count(string(calls), "shared\n"); got != 2 {
		t.Fatalf("shell mode shared account called %d times across different cwds", got)
	}
}

func TestHelperCwdKeyForRelativeResolution(t *testing.T) {
	cmd := config.Command{Executable: "/usr/bin/claude"}
	env := map[string]string{"PATH": "/usr/bin:/bin"}
	if got := helperCwdKey(cmd, env, "/work/a"); got != "" {
		t.Fatalf("absolute direct invocation should share: %q", got)
	}
	for _, path := range []string{"", "./bin:/usr/bin", "/usr/bin:"} {
		env["PATH"] = path
		if got := helperCwdKey(cmd, env, "/work/a"); got != "/work/a" {
			t.Fatalf("PATH %q must isolate cwd, got %q", path, got)
		}
	}
	env["PATH"] = "/usr/bin:/bin"
	cmd.Args = []string{"./launcher"}
	if got := helperCwdKey(cmd, env, "/work/a"); got != "/work/a" {
		t.Fatalf("relative command argument must isolate cwd, got %q", got)
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

func TestReadSnapshotMarksStaleObservation(t *testing.T) {
	current := time.Now().UTC().Format(time.RFC3339Nano)
	old := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	s := &State{RuntimeSeen: current, Members: map[string]*Member{
		"fresh":   {State: MemberStateWorking, ObservedAt: current},
		"old":     {State: MemberStateIdle, ObservedAt: old},
		"stopped": {State: MemberStateStopped},
	}}
	markObservationStaleness(s)
	if !s.ObservationStale || s.Members["fresh"].ObservationStale || !s.Members["old"].ObservationStale || s.Members["stopped"].ObservationStale {
		t.Fatalf("member observation ages were not distinguished: %+v", s.Members)
	}
	s.RuntimeSeen = old
	s.Members["old"].ObservedAt = current
	markObservationStaleness(s)
	if !s.Members["fresh"].ObservationStale || !s.Members["old"].ObservationStale || s.Members["stopped"].ObservationStale {
		t.Fatalf("dead runtime did not mark live members stale: %+v", s.Members)
	}
}
