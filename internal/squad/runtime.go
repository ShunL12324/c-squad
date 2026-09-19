package squad

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func attach(st *Store, id string) error {
	s, e := st.read()
	if e != nil {
		return e
	}
	if !s.Active || masterGone(s) {
		return fmt.Errorf("team %q is stopped; run csquad start --name %s to resume it", s.ID, s.ID)
	}
	m, e := s.member(id)
	if e != nil {
		return e
	}
	cmd := "attach-session"
	if env := os.Getenv("TMUX"); env != "" && strings.Split(env, ",")[0] == s.Socket {
		cmd = "switch-client"
	}
	c := exec.Command("tmux", "-S", s.Socket, cmd, "-t", "="+m.Session)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if cmd == "attach-session" {
		c.Env = agentenv.Environ(map[string]string{"TMUX": ""})
	}
	err := c.Run()
	if latest, readErr := st.read(); readErr == nil && !latest.Active {
		_, _ = fmt.Fprintf(os.Stderr, "Team %s stopped (%s). All team sessions are closed; task records are saved.\nRestart: csquad start --name %s\n", latest.ID, latest.StopReason, latest.ID)
	}
	return err
}
func runEngine(st *Store, actor string, gen int, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing engine argv")
	}
	s, e := st.read()
	if e != nil {
		return e
	}
	m, e := s.member(actor)
	if e != nil {
		return e
	}
	if gen != m.Generation || !s.Active {
		return ErrStaleGeneration
	}
	c := exec.Command(args[0], args[1:]...)
	c.Dir = m.Cwd
	c.Env = agentenv.Environ(m.Env)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	all, err := process.Snapshot()
	if err != nil {
		return err
	}
	e = st.update(func(s *State) error {
		m := s.Members[actor]
		m.RunnerPID = os.Getpid()
		m.ProcessStart = all[os.Getpid()].Start
		return nil
	})
	if e != nil {
		return e
	}
	e = c.Start()
	if e == nil {
		started, _ := process.Snapshot()
		if err = st.update(func(s *State) error {
			s.Members[actor].EnginePID = c.Process.Pid
			if p, ok := started[c.Process.Pid]; ok {
				s.Members[actor].Processes = append(s.Members[actor].Processes, p)
			}
			return nil
		}); err != nil {
			_ = c.Process.Kill()
			_ = c.Wait()
			return err
		}
		startupDone := make(chan struct{})
		startupFinished := make(chan struct{})
		cfg, cfgErr := s.effectiveConfig()
		if cfgErr == nil && cfg.Bypass && m.Engine == config.Claude {
			go func() { defer close(startupFinished); st.monitorClaudeStartup(actor, gen, startupDone) }()
		} else {
			close(startupFinished)
		}
		e = c.Wait()
		close(startupDone)
		<-startupFinished
	}
	state := MemberStateStopped
	if e != nil {
		state = MemberStateCrashed
	}
	stateErr := st.update(func(s *State) error {
		if m := s.Members[actor]; m != nil && m.Generation == gen && m.State != MemberStateRemoved {
			m.State = state
			detail := "engine exited normally"
			if e != nil {
				detail = e.Error()
			}
			s.event(actor, string(state), detail)
		}
		return nil
	})
	if actor == "master" {
		if s, err := st.read(); err == nil && s.Active && s.Members[actor].Generation == gen {
			_ = requestShutdown(st, s, "master_exit")
		}
	}
	return errors.Join(e, stateErr)
}
