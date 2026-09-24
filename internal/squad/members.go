package squad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func memberCommand(st *Store, actor string, p []string, o options) error {
	if len(p) == 0 {
		return errors.New("member subcommand required")
	}
	if p[0] == "list" {
		s, e := st.read()
		if e != nil {
			return e
		}
		markObservationStaleness(s)
		return queryOut(o, s.Members)
	}
	if p[0] == "profiles" {
		s, e := st.read()
		if e != nil {
			return e
		}
		cfg, warning, e := s.liveConfig()
		if e != nil {
			return e
		}
		if warning != "" {
			fmt.Fprintln(os.Stderr, "Profile warning:", warning)
		}
		return queryOut(o, newProfileListing(cfg))
	}
	if len(p) < 2 {
		return errors.New("member ID required")
	}
	id := p[1]
	if p[0] == "inspect" {
		s, e := st.read()
		if e != nil {
			return e
		}
		m, e := s.member(id)
		if e != nil {
			return e
		}
		out, _ := tm(s, "capture-pane", "-p", "-t", agentPane(m), "-S", "-80")
		// The card truncates long branch names to fit a 28-column panel; this is
		// where the untruncated value lives. It is resolved on demand rather than
		// stored, so the ledger keeps no Git state to go stale.
		// Instructions are the reason a member exists, so they are surfaced at the
		// top level rather than buried in the record. The member copy drops them:
		// printing the same prose twice leaves a reader unsure which one is live.
		visible := *m
		visible.Instructions = ""
		record := map[string]any{"member": &visible, "instructions": m.Instructions, "terminal": out}
		if state, ok := memberGit(m.Cwd); ok {
			record["git"] = state
		}
		return queryOut(o, record)
	}
	if actor != "master" {
		return fmt.Errorf("only master manages members: %w", ErrMasterRequired)
	}
	if p[0] == "set-cwd" {
		return setStoppedMemberDirectory(st, actor, id, o["cwd"])
	}
	if p[0] == "add" {
		unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
		if e != nil {
			return e
		}
		defer unlock()
		current, e := st.read()
		if e != nil {
			return e
		}
		if !current.Active {
			return ErrTeamStopped
		}
		if !validID.MatchString(id) || id == "master" || id == UserSender {
			return errors.New("invalid/reserved member ID")
		}
		// A profile added to the configuration while the team runs is usable now.
		s, e := st.refreshProfiles()
		if e != nil {
			return e
		}
		cfg, e := s.effectiveConfig()
		if e != nil {
			return e
		}
		if e = rejectRemovedLaunchFlags(o); e != nil {
			return e
		}
		p, profile, e := cfg.ResolveProfile(o["profile"], false)
		if e != nil {
			return e
		}
		env := profileEnv(p)
		if e = preflight.CheckCommand(p.Engine, p.LaunchCommand(p.Engine), env); e != nil {
			return e
		}
		cwd := s.Root
		if o["task"] != "" {
			task, e := s.task(o["task"])
			if e != nil {
				return e
			}
			if task.Workspace != "" {
				cwd = task.Workspace
			}
		}
		cwd, e = memberDirectory(cwd, o["cwd"])
		if e != nil {
			return e
		}
		e = st.update(func(s *State) error {
			if s.Members[id] != nil {
				return errors.New("member exists; use replace")
			}
			count := 0
			for _, m := range s.Members {
				if m.State != MemberStateRemoved {
					count++
				}
			}
			if count >= cfg.MaxMembers {
				return ErrMemberLimit
			}
			// Engine, model and environment are materialised now, so later config
			// edits never change an existing member. The profile name is kept
			// because the launch command is resolved through it at every start.
			s.Members[id] = &Member{Color: tmux.Color(o["color"]), Instructions: o["instructions"], Env: env, ID: id, Engine: p.Engine, Model: p.Model, Profile: profile, Session: "csq-" + s.ID + "-" + id, Cwd: cwd, State: MemberStateStarting, Generation: 1}
			if o["task"] != "" {
				t := s.Tasks[o["task"]]
				t.Participants = append(t.Participants, id)
				noticeCrossRepo(s, actor, t, s.Members[id])
			}
			s.event(actor, "member_added", id)
			return nil
		})
		if e != nil {
			return e
		}
		if e = st.launch(id, false, o["prompt"]); e != nil {
			stateErr := st.update(func(s *State) error {
				s.Members[id].State = MemberStateCrashed
				s.event(id, "launch_failed", e.Error())
				return nil
			})
			return errors.Join(e, stateErr)
		}
		return jsonOut(map[string]string{"member": id, "state": "starting"})
	}
	s, e := st.read()
	if e != nil {
		return e
	}
	m, e := s.member(id)
	if e != nil {
		return e
	}
	if p[0] == "interrupt" {
		_, e = tm(s, "send-keys", "-t", agentPane(m), "Escape")
		return e
	}
	if p[0] != "remove" && p[0] != "restart" && p[0] != "replace" {
		return errors.New("unknown member operation")
	}
	return lifecycle(st, actor, p[0], id, o)
}

// memberDirectory resolves explicit paths in the caller's directory. An omitted
// override retains the task workspace or the member's existing launch directory.
func memberDirectory(fallback, override string) (string, error) {
	if override == "" {
		return fallback, nil
	}
	path, err := filepath.Abs(override)
	if err != nil {
		return "", fmt.Errorf("member working directory: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("member working directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("member working directory %q is not a directory", path)
	}
	return path, nil
}
