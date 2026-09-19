package squad

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/process"
)

func lifecycle(st *Store, actor, op, id, initial, directory string) error {
	if actor != "master" {
		return fmt.Errorf("only master manages members: %w", ErrMasterRequired)
	}
	if id == "master" && (op == "remove" || st.Generation > 0) {
		return errors.New("recover master from an outside CLI: csquad --team DIR recover [--fresh]")
	}
	teamUnlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if e != nil {
		return e
	}
	defer teamUnlock()
	unlock, e := filelock.Acquire(st.Dir, "member-"+id, false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	m, e := s.member(id)
	if e != nil {
		return e
	}
	if m.State == MemberStateRemoved && op != "replace" {
		return errors.New("member removed; use replace")
	}
	if !s.Active {
		return fmt.Errorf("use csquad resume to recover the whole team: %w", ErrTeamStopped)
	}
	cwd, e := memberDirectory(m.Cwd, directory)
	if e != nil {
		return e
	}
	if op != "remove" {
		if err := preflight.Check(m.Engine); err != nil {
			return err
		}
	}
	// Save an auditable handoff before stopping anything. Never reset Git state.
	handoff := map[string]any{"member": m, "tasks": map[string]any{}, "questions": s.Questions}
	tasks := handoff["tasks"].(map[string]any)
	for _, t := range s.Tasks {
		if contains(t.Participants, id) || id == "master" {
			entry := map[string]any{"task": t}
			if t.Workspace != "" {
				entry["head"], _ = git(t.Workspace, "rev-parse", "HEAD")
				entry["changes"], _ = git(t.Workspace, "status", "--porcelain")
			}
			tasks[t.ID] = entry
		}
	}
	dir := filepath.Join(st.Dir, "handoffs")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	path := filepath.Join(dir, id+"-"+strconv.Itoa(m.Generation)+".json")
	b, _ := json.MarshalIndent(handoff, "", "  ")
	if e = os.WriteFile(path, b, 0600); e != nil {
		return e
	}
	// Fence ledger writes before stopping the old tree. Failure leaves the member
	// unavailable, never starts a second writer, and preserves the old identity.
	e = st.update(func(s *State) error {
		v := s.Members[id]
		if v.State == MemberStateRemoved && s.Config != nil {
			count := 0
			for _, other := range s.Members {
				if other.State != MemberStateRemoved {
					count++
				}
			}
			if count >= s.Config.MaxMembers {
				return ErrMemberLimit
			}
		}
		v.Generation++
		v.State = MemberStateStopping
		v.Handoff = path
		v.Peer = ""
		s.event(actor, "stopping", id)
		return nil
	})
	if e != nil {
		return e
	}
	if e = killMember(st, id); e != nil {
		stateErr := st.update(func(s *State) error {
			s.Members[id].State = MemberStateNeedsAttention
			s.event(actor, "stop_failed", id+": "+e.Error())
			return nil
		})
		return errors.Join(e, stateErr)
	}
	e = st.update(func(s *State) error {
		v := s.Members[id]
		v.resetRuntime()
		v.Cwd = cwd
		if op == "replace" {
			v.EngineID = ""
		}
		if op == "remove" {
			v.State = MemberStateRemoved
			for _, t := range s.Tasks {
				if t.State == TaskPhaseDone {
					continue
				}
				if t.Owner == id {
					t.Owner = ""
				}
				a := []string{}
				for _, p := range t.Participants {
					if p != id {
						a = append(a, p)
					}
				}
				t.Participants = a
			}
		}
		for _, msg := range s.Messages {
			if msg.To == id && msg.State != DeliveryStateAcknowledged {
				msg.resetDelivery()
			}
		}
		s.event(actor, op, id)
		return nil
	})
	if e != nil {
		return e
	}
	if op == "remove" {
		if e = st.configureNavigation(); e != nil {
			return e
		}
		return jsonOut(map[string]string{"member": id, "state": "removed", "handoff": path})
	}

	if e = st.launch(id, op == "restart", initial); e != nil {
		stateErr := st.update(func(s *State) error {
			s.Members[id].State = MemberStateCrashed
			s.event(actor, "launch_failed", e.Error())
			return nil
		})
		return errors.Join(e, stateErr)
	}
	return st.startRuntime()
}

func killMember(st *Store, id string) error {
	s, e := st.read()
	if e != nil {
		return e
	}
	m, e := s.member(id)
	if e != nil {
		return e
	}
	if _, err := tm(s, "has-session", "-t", "="+m.Session); err == nil {
		owner, tagErr := tm(s, "show-options", "-v", "-t", "="+m.Session, "@csquad_team")
		if tagErr == nil && owner != st.Dir {
			return fmt.Errorf("refusing to stop tmux session owned by another team: %s", m.Session)
		}
		if tagErr != nil && m.Pane == "" && m.RunnerPID == 0 {
			return fmt.Errorf("refusing to stop unowned tmux session: %s", m.Session)
		}
	}
	root := m.RunnerPID
	start := m.ProcessStart
	if root == 0 {
		if _, err := tm(s, "has-session", "-t", "="+m.Session); err != nil {
			return nil
		}
		out, e := tm(s, "display-message", "-p", "-t", agentPane(m), "#{pane_pid} #{pane_dead}")
		if e != nil {
			return nil
		}
		f := strings.Fields(out)
		if len(f) != 2 {
			return fmt.Errorf("cannot identify member process")
		}
		if f[1] == "0" {
			root, _ = strconv.Atoi(f[0])
		}
	}
	if root > 0 && start != "" {
		all, err := process.Snapshot()
		if err != nil {
			return err
		}
		if p, ok := all[root]; !ok || p.Start != start {
			root = 0
		}
	}
	if root > 0 {
		if e = process.StopTree(root, start); e != nil {
			return e
		}
	}
	// A wrapper can die before its child. Retain observed descendants across
	// refreshes and terminate surviving identities even after reparenting.
	for _, p := range m.Processes {
		all, err := process.Snapshot()
		if err != nil {
			return err
		}
		if process.Alive(p, all) {
			if err = process.StopTree(p.PID, p.Start); err != nil {
				return err
			}
		}
	}
	if _, e = tm(s, "has-session", "-t", "="+m.Session); e == nil {
		_, e = tm(s, "kill-session", "-t", "="+m.Session)
		return e
	}
	return nil
}
