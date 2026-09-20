package squad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func memberCommand(st *Store, actor string, p []string, o options) error {
	if len(p) == 0 {
		return errors.New("member subcommand required")
	}
	if p[0] == "list" {
		if e := st.refresh(); e != nil {
			return e
		}
		s, e := st.read()
		if e != nil {
			return e
		}
		return jsonOut(s.Members)
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
		return jsonOut(map[string]any{"member": m, "terminal": out})
	}
	if actor != "master" {
		return fmt.Errorf("only master manages members: %w", ErrMasterRequired)
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
		if !validID.MatchString(id) || id == "master" {
			return errors.New("invalid/reserved member ID")
		}
		s, e := st.read()
		if e != nil {
			return e
		}
		cfg, e := s.effectiveConfig()
		if e != nil {
			return e
		}
		role := o["role"]
		if role == "" {
			role = "team member"
		}
		t := config.EngineDefaults(cfg, false)
		// Keep explicitly requested legacy templates usable by existing masters.
		if name := o["template"]; name != "" {
			legacy, ok := cfg.Templates[name]
			if !ok {
				return fmt.Errorf("unknown legacy template %q; use --role and --instructions", name)
			}
			t = legacy
			if o["role"] == "" {
				role = name
			}
		}
		if o["instructions"] != "" {
			t.Prompt = o["instructions"]
		}
		if o["engine"] != "" && config.Engine(o["engine"]) != t.Engine {
			t.Engine = config.Engine(o["engine"])
			t.Model = ""
		}
		if o["model"] != "" {
			t.Model = o["model"]
		}
		if e = preflight.Check(t.Engine); e != nil {
			return e
		}
		overrides, e := agentenv.Parse(o["env"])
		if e != nil {
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
			s.Members[id] = &Member{Color: tmux.Color(o["color"]), Instructions: t.Prompt, Env: memberEnv(cfg, t, overrides), ID: id, Engine: t.Engine, Model: t.Model, Role: role, Session: "csq-" + s.ID + "-" + id, Cwd: cwd, State: MemberStateStarting, Generation: 1}
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
	return lifecycle(st, actor, p[0], id, o["prompt"], o["cwd"])
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
