package squad

import (
	"encoding/json"
	"strings"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func (st *Store) refresh() error { _, err := st.observe(); return err }

// observe refreshes member state and reports which members this pass actually
// observed. A member missing from the result is unknown: a Claude session the
// agents helper failed to report keeps its previous state, which proves
// nothing about whether it is still working.
func (st *Store) observe() (map[string]bool, error) {
	s, e := st.read()
	if e != nil {
		return nil, e
	}
	helperOK := map[string]bool{}
	var claude []struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
		Waiting   string `json:"waitingFor"`
	}
	for _, member := range s.Members {
		if member.Engine == config.Claude {
			if out, err := s.engineHelper(member, "agents", "--json"); err == nil {
				var entries []struct {
					SessionID string `json:"sessionId"`
					Status    string `json:"status"`
					Waiting   string `json:"waitingFor"`
				}
				if json.Unmarshal([]byte(out), &entries) == nil {
					helperOK[member.ID] = true
					for _, entry := range entries {
						if entry.SessionID == member.EngineID {
							claude = append(claude, entry)
						}
					}
				}
			}
		}
	}
	observed, procErr := process.Snapshot()
	var known map[string]bool
	err := st.update(func(cur *State) error {
		known = map[string]bool{}
		for _, m := range cur.Members {
			if old := s.Members[m.ID]; old == nil || old.Generation != m.Generation {
				continue
			}
			if m.State == MemberStateRemoved || m.State == MemberStateStopped {
				known[m.ID] = true
				continue
			}
			if m.State == MemberStateStopping || m.State == MemberStateNeedsAttention {
				continue
			}
			if procErr == nil && m.RunnerPID > 0 {
				tracked := map[int]process.Identity{}
				for _, p := range m.Processes {
					if process.Alive(p, observed) {
						tracked[p.PID] = p
					}
				}
				if root, ok := observed[m.RunnerPID]; ok && root.Start == m.ProcessStart {
					for id, p := range process.Descendants(observed, m.RunnerPID) {
						tracked[id] = p
					}
				}
				m.Processes = nil
				for _, p := range tracked {
					m.Processes = append(m.Processes, p)
				}
			}
			m.ObservedAt = now()
			out, err := tm(s, "display-message", "-p", "-t", agentPane(m), "#{pane_dead}")
			// A restart publishes its new generation before creating the tmux pane.
			if err != nil && m.State == MemberStateStarting {
				continue
			}
			if err != nil || out == "1" {
				m.State = MemberStateCrashed
				known[m.ID] = true
				continue
			}
			known[m.ID] = m.Engine != config.Claude
			if m.Engine == config.Codex && m.State == MemberStateStarting {
				if pane, err := tm(s, "capture-pane", "-p", "-t", agentPane(m)); err == nil && codexEmptyComposer(pane) {
					m.State = MemberStateIdle
				}
			}
			for _, c := range claude {
				if c.SessionID == m.EngineID {
					known[m.ID] = helperOK[m.ID]
					switch c.Status {
					case "busy":
						m.State = MemberStateWorking
					case "idle":
						if m.State != MemberStateWaitingMaster {
							m.State = MemberStateIdle
						}
					case "waiting":
						m.State = MemberState("waiting_" + strings.ReplaceAll(c.Waiting, " ", "_"))
					}
				}
			}
			for _, q := range cur.Questions {
				if q.Member == m.ID && q.State == QuestionStateOpen {
					m.State = MemberStateWaitingMaster
				}
			}
		}
		return nil
	})
	return known, err
}
