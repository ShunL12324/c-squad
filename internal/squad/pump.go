package squad

import (
	"errors"
	"fmt"
	"time"

	"github.com/ShunL12324/c-squad/internal/filelock"
)

// runtimeProtocol must change whenever the persisted ledger schema changes. A
// runtime left from an older binary rewrites the whole ledger with its own
// structs, so keeping it alive after an upgrade silently drops new fields.
const runtimeProtocol = "9"

func runtimeName(s *State) string { return "csq-" + s.ID + "-runtime" }
func (st *Store) startRuntime() error {
	unlock, e := filelock.Acquire(st.Dir, "runtime-start", false)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := st.read()
	if e != nil {
		return e
	}
	if !s.Active {
		return ErrTeamStopped
	}
	// Only the team's own build may replace its runtime; a newer caller would
	// otherwise see a protocol mismatch and restart it on every call.
	if !isPinnedBuild(s) {
		return nil
	}
	if e = ensurePin(s); e != nil {
		return e
	}
	// Refresh the exit hook without restarting the native Master.
	if _, err := tm(s, "has-session", "-t", "="+s.Members["master"].Session); err == nil {
		if e = installMasterHook(st); e != nil {
			return e
		}
	}
	name := runtimeName(s)
	if dead, err := tm(s, "display-message", "-p", "-t", "="+name+":", "#{pane_dead}"); err == nil {
		if dead == "0" {
			protocol, _ := tm(s, "show-options", "-v", "-t", "="+name, "@csquad_runtime_protocol")
			if protocol == runtimeProtocol {
				return nil
			}
		}
		if _, e = tm(s, "kill-session", "-t", "="+name); e != nil {
			return e
		}
	}
	_, e = tm(s, "new-session", "-d", "-s", name, "-c", s.Root, s.Executable, "--team", st.Dir, "--member", "master", "--generation", "0", "runtime")
	if e == nil {
		_, e = tm(s, "set-option", "-t", "="+name, "@csquad_runtime_protocol", runtimeProtocol)
	}
	return e
}
func (st *Store) runRuntime() error {
	unlock, e := filelock.Acquire(st.Dir, "runtime", true)
	if e != nil {
		return fmt.Errorf("runtime already running: %w", e)
	}
	defer unlock()
	var stall stallTimer
	for {
		passStart := time.Now()
		s, e := st.read()
		if e != nil {
			return e
		}
		if !s.Active {
			return nil
		}
		if err := st.checkMaster(); err != nil {
			return err
		}
		cycleErr := errors.Join(st.expireStall(&stall), st.syncMessages(), st.reconcile())
		for _, t := range s.Tasks {
			if t.State == TaskPhasePreparing {
				cycleErr = errors.Join(cycleErr, st.prepareWorkspace(t.ID))
			}
		}
		known, refreshErr := st.observe()
		cycleErr = errors.Join(cycleErr, refreshErr, st.checkStall(&stall, known, refreshErr, passStart))
		if err := st.update(func(s *State) error {
			s.RuntimeSeen = now()
			if cycleErr != nil {
				s.event("runtime", "cycle_error", cycleErr.Error())
			}
			return nil
		}); err != nil {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}
