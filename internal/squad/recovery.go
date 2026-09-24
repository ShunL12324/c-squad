package squad

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/preflight"
)

func projectBase(root string) string {
	if v := os.Getenv("CSQUAD_HOME"); v != "" {
		return v
	}
	return filepath.Join(root, ".csquad")
}
func currentProjectBase() string {
	cwd, e := os.Getwd()
	if e != nil {
		return stateBase()
	}
	if root, e := git(cwd, "rev-parse", "--show-toplevel"); e == nil {
		return projectBase(root)
	}
	// Non-Git projects are discoverable from their subdirectories too.
	for p := cwd; ; p = filepath.Dir(p) {
		if _, e := os.Stat(filepath.Join(p, ".csquad", "last-team")); e == nil {
			return projectBase(p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return projectBase(cwd)
}
func excludeProjectState(root string) error {
	path, e := git(root, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if e != nil {
		return nil
	}
	unlock, e := filelock.Acquire(projectBase(root), "git-exclude", false)
	if e != nil {
		return e
	}
	defer unlock()
	b, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "/.csquad/" {
			return nil
		}
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	_, e = f.WriteString("\n# C-Squad local recovery state\n/.csquad/\n")
	return e
}
func shutdownCommand(st *Store, s *State, reason string) string {
	// Only ever run through run-shell, which expands formats.
	return runShellQuote(s.Executable) + " --team " + runShellQuote(st.Dir) + " --member master --generation 0 shutdown --epoch " + strconv.Itoa(s.Epoch) + " --expected-generation " + strconv.Itoa(s.Members["master"].Generation) + " --reason " + runShellQuote(reason)
}
func requestShutdown(st *Store, s *State, reason string) error {
	_, e := tm(s, "run-shell", "-b", shutdownCommand(st, s, reason))
	return e
}
func shutdownRequest(st *Store, o options) error {
	unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	epoch, e := strconv.Atoi(o["epoch"])
	if e != nil {
		return errors.New("shutdown requires epoch")
	}
	gen, e := strconv.Atoi(o["expected-generation"])
	if e != nil {
		return errors.New("shutdown requires expected generation")
	}
	if s.Epoch != epoch || s.Members["master"].Generation != gen {
		return nil
	}
	if !s.Active && s.Phase != TeamPhaseStopping && s.Phase != TeamPhaseCleanupFailed {
		return nil
	}
	return cleanupTeam(st, o["reason"])
}

// Caller owns team-lifecycle. Final state is committed before stopping runtime:
// cleanup may itself be a tmux job, and must not rely on surviving server exit.
func cleanupTeam(st *Store, reason string) error {
	s, e := st.read()
	if e != nil {
		return e
	}
	if reason == "" {
		reason = "interrupted"
	}
	if e = st.update(func(s *State) error {
		s.Active = false
		s.Phase = TeamPhaseStopping
		s.StopReason = reason
		for _, m := range s.Members {
			if m.State != MemberStateRemoved {
				m.Generation++
				m.State = MemberStateStopping
				m.Peer = ""
			}
		}
		s.event("master", "team_stopping", reason)
		return nil
	}); e != nil {
		return e
	}
	ids := []string{}
	for id, m := range s.Members {
		if id != "master" && m.State != MemberStateRemoved {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	ids = append(ids, "master")
	var failures []error
	for _, id := range ids {
		unlock, err := filelock.Acquire(st.Dir, "member-"+id, false)
		if err == nil {
			err = killMember(st, id)
			unlock()
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
		}
		updateErr := st.update(func(cur *State) error {
			m := cur.Members[id]
			if err == nil {
				m.State = MemberStateStopped
			} else {
				m.State = MemberStateNeedsAttention
				cur.event(id, "cleanup_failed", err.Error())
			}
			return nil
		})
		if updateErr != nil {
			failures = append(failures, updateErr)
		}
	}
	if e = os.RemoveAll(filepath.Join(st.Dir, "runtime")); e != nil {
		failures = append(failures, e)
	}
	st.clearNavigation(s)
	e = st.update(func(cur *State) error {
		cur.Phase = TeamPhaseInterrupted
		if reason == "stopped" {
			cur.Phase = TeamPhaseStopped
		}
		if len(failures) > 0 {
			cur.Phase = TeamPhaseCleanupFailed
		}
		cur.event("master", "team_cleanup", string(cur.Phase))
		return nil
	})
	if e != nil {
		failures = append(failures, e)
	}
	// Only an owned server can be destroyed; a shared server keeps unrelated sessions.
	sessions, _ := tm(s, "list-sessions", "-F", "#{session_name}")
	if s.OwnSocket && len(failures) == 0 && (sessions == runtimeName(s) || sessions == "") {
		// The final ledger state is committed before this potentially self-terminating call.
		_, _ = tm(s, "kill-server")
	} else {
		// An absent runtime session is already cleaned up.
		_, _ = tm(s, "kill-session", "-t", "="+runtimeName(s))
	}
	return errors.Join(failures...)
}

func masterGone(s *State) bool {
	m := s.Members["master"]
	if m == nil {
		return true
	}
	if m.State == MemberStateCrashed || m.State == MemberStateStopped {
		return true
	}
	out, e := tm(s, "display-message", "-p", "-t", agentPane(m), "#{pane_dead}")
	return e != nil || out == "1"
}
func (st *Store) checkMaster() error {
	unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", true)
	if e != nil {
		return nil
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	if s.Active && s.Phase != TeamPhaseStarting && masterGone(s) {
		return requestShutdown(st, s, "master_exit")
	}
	return nil
}

func resumeTeam(st *Store, o options) error {
	if err := rejectRemovedLaunchFlags(o); err != nil {
		return err
	}
	unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	if s.Active && !masterGone(s) {
		return errors.New("team is running; use csquad attach")
	}
	currentConfig, err := config.Load(s.Root)
	if err != nil {
		return err
	}
	if err := validateResumeDirectories(s); err != nil {
		return err
	}
	var drifted []string
	for _, member := range s.Members {
		if member.State != MemberStateRemoved {
			resumed := *member
			resumed.Env = agentenv.Merge(member.Env)
			saved, current := resumeProfileEnv(s.Config, currentConfig, member)
			resumed.applyResumeEnvironment(saved, current)
			if profileDrift(currentConfig, &resumed, saved) {
				drifted = append(drifted, member.ID)
			}
			command, warning := currentConfig.ProfileCommand(member.Profile, member.Engine)
			if warning != "" {
				fmt.Fprintln(os.Stderr, warning)
			}
			if err := preflight.CheckCommand(member.Engine, command, resumed.Env); err != nil {
				return err
			}
		}
	}
	if notice := profileDriftNotice(drifted); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	// Move the team to this build first, in the one transaction allowed to
	// write before this process is the pin. Everything above only read. From
	// here on this process is the team's build: the reaping, reconcile and
	// relaunch below pass the self check, and any process left from the old
	// build is refused if it writes before it is reaped.
	if e = decideRepin(s); e != nil {
		return e
	}
	pinned, e := st.transitionPin()
	if e != nil {
		return fmt.Errorf("pin this csquad build for the team: %w", e)
	}
	// Reap old process identities before changing socket or clearing PID records.
	if e = cleanupTeam(st, "interrupted"); e != nil {
		return fmt.Errorf("old team cleanup failed; recovery refused: %w", e)
	}
	if e = st.reconcile(); e != nil {
		return e
	}
	s, e = st.read()
	if e != nil {
		return e
	}
	handoff := filepath.Join(st.Dir, "handoffs", fmt.Sprintf("team-%d.json", s.Epoch))
	if e = os.MkdirAll(filepath.Dir(handoff), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(handoff, b, 0600); e != nil {
		return e
	}
	random := make([]byte, 4)
	if _, e = rand.Read(random); e != nil {
		return e
	}
	binary := pinned.Path
	if e = st.update(func(cur *State) error {
		// The profiles a member inherited from are kept aside before the snapshot
		// adopts the current tables, so an edit shows up as a difference instead of
		// being compared against itself.
		var saved *config.Config
		if cur.Config != nil {
			previous := *cur.Config
			saved = &previous
		}
		refreshProfileTables(cur, currentConfig)

		cur.Epoch++
		cur.Active = true
		cur.Phase = TeamPhaseStarting
		cur.StopReason = ""
		cur.RuntimeSeen = ""
		cur.Executable = binary
		cur.Socket = filepath.Join(os.TempDir(), fmt.Sprintf("csq-%d-%s.sock", os.Getuid(), hex.EncodeToString(random)))
		cur.OwnSocket = true
		for _, m := range cur.Members {
			if m.State == MemberStateRemoved {
				continue
			}
			m.applyResumeEnvironment(resumeProfileEnv(saved, currentConfig, m))
			m.Generation++
			m.resetRuntime()
			m.Handoff = handoff
			if o["fresh"] == "true" {
				m.EngineID = ""
			}
		}
		for _, msg := range cur.Messages {
			if msg.State != DeliveryStateAcknowledged {
				if m := cur.Members[msg.To]; m != nil && m.State != MemberStateRemoved {
					msg.recoverDelivery()
				}
			}
		}
		cur.queueRecoveryNotices()
		cur.event("master", "team_resumed", handoff)
		return nil
	}); e != nil {
		return e
	}
	s, e = st.read()
	if e != nil {
		return e
	}
	ids := []string{"master"}
	others := []string{}
	for id, m := range s.Members {
		if id != "master" && m.State != MemberStateRemoved {
			others = append(others, id)
		}
	}
	sort.Strings(others)
	ids = append(ids, others...)
	for _, id := range ids {
		initial := ""
		if id == "master" {
			initial = o["prompt"]
		}
		if e = st.launch(id, true, initial); e != nil {
			return errors.Join(e, cleanupTeam(st, "resume_failed"))
		}
	}
	if e = st.update(func(s *State) error { s.Phase = TeamPhaseRunning; return nil }); e != nil {
		return e
	}
	if e = st.startRuntime(); e != nil {
		return errors.Join(e, cleanupTeam(st, "resume_failed"))
	}
	if e = os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(st.Dir)), "last-team"), []byte(st.Dir), 0600); e != nil {
		return e
	}
	unlock()
	if o["detach"] == "true" {
		return jsonOut(map[string]string{"team": st.Dir, "state": "resumed", "attach": binary + " --team " + st.Dir + " attach"})
	}
	return attach(st, "master")
}

// Repair leftovers on the next invocation even if both tmux and runtime died.
func reapProjectTeams(base string) error {
	entries, e := os.ReadDir(filepath.Join(base, "teams"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(base, "teams", entry.Name())
		if _, e = os.Stat(filepath.Join(dir, "state.db")); e != nil {
			continue
		}
		st, err := openStore(dir)
		if err != nil {
			return err
		}
		unlock, err := filelock.Acquire(dir, "team-lifecycle", true)
		if err != nil {
			_ = st.DB.Close()
			continue
		}
		s, err := st.read()
		if err == nil && !isPinnedBuild(s) {
			// Another build owns that team; its own csquad cleans it up.
			fmt.Fprintf(os.Stderr, "csquad: team %s is pinned to another csquad build; not cleaning it up from this one\n", s.ID)
			unlock()
			_ = st.DB.Close()
			continue
		}
		if err == nil && ((s.Active && masterGone(s)) || s.Phase == TeamPhaseStopping || s.Phase == TeamPhaseCleanupFailed) {
			err = cleanupTeam(st, "interrupted")
		}
		unlock()
		_ = st.DB.Close()
		if err != nil {
			return fmt.Errorf("cleanup previous team %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func installMasterHook(st *Store) error {
	s, err := st.read()
	if err != nil {
		return err
	}
	m := s.Members["master"]
	if m == nil {
		return errors.New("missing master")
	}
	_, e := tm(s, "set-hook", "-w", "-t", "="+m.Session+":", "pane-died", "if-shell -F "+shellQuote("#{==:#{hook_pane},"+m.Pane+"}")+" "+shellQuote("run-shell -b "+shellQuote(shutdownCommand(st, s, "master_exit"))))
	return e
}
