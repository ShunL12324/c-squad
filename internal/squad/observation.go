package squad

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func (st *Store) refresh() error { _, err := st.observe(); return err }

// markObservationStaleness annotates a read snapshot without running native
// probes or updating the ledger. The runtime normally observes every two
// seconds; 30 seconds gives slow passes room while revealing dead runtimes.
func markObservationStaleness(s *State) {
	cutoff := time.Now().Add(-30 * time.Second)
	stale := func(value string) bool {
		at, err := time.Parse(time.RFC3339Nano, value)
		return err != nil || at.Before(cutoff)
	}
	runtimeStale := stale(s.RuntimeSeen)
	s.ObservationStale = runtimeStale
	for _, m := range s.Members {
		m.ObservationStale = false
		if m.State == MemberStateRemoved || m.State == MemberStateStopped {
			continue
		}
		m.ObservationStale = runtimeStale || stale(m.ObservedAt)
		s.ObservationStale = s.ObservationStale || m.ObservationStale
	}
}

// A native direct invocation can share an account-wide agents listing across
// worktrees when neither command resolution nor its arguments depend on cwd.
// Shell startup files can inspect cwd, so shell mode always isolates them.
func helperCwdKey(command config.Command, env map[string]string, cwd string) string {
	if command.Shell != "" {
		return cwd
	}
	path, overridden := env["PATH"]
	if !overridden {
		path = os.Getenv("PATH")
	}
	for _, entry := range strings.Split(path, string(os.PathListSeparator)) {
		if !filepath.IsAbs(entry) {
			return cwd
		}
	}
	for _, arg := range command.Args {
		if strings.ContainsRune(arg, '/') && !filepath.IsAbs(arg) {
			return cwd
		}
	}
	return ""
}

type claudeEntry struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Waiting   string `json:"waitingFor"`
}

// Interactive shells may print startup text before their helper runs. Only
// shell mode skips complete leading lines; both modes require one complete
// JSON array with no non-whitespace suffix.
func decodeClaudeAgents(out string, shell bool) ([]claudeEntry, error) {
	payload := out
	if shell {
		offset, found := 0, false
		for _, line := range strings.SplitAfter(out, "\n") {
			leading := len(line) - len(strings.TrimLeft(line, " \t\r"))
			if strings.HasPrefix(line[leading:], "[") {
				payload = out[offset+leading:]
				found = true
				break
			}
			offset += len(line)
		}
		if !found {
			return nil, fmt.Errorf("Claude agents JSON array not found")
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(payload), "[") {
		return nil, fmt.Errorf("Claude agents response is not a JSON array")
	}
	var entries []claudeEntry
	if err := json.Unmarshal([]byte(payload), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// observe refreshes member state and reports which members this pass actually
// observed. A member missing from the result is unknown: a Claude session the
// agents helper failed to report keeps its previous state, which proves
// nothing about whether it is still working.
func (st *Store) observe() (map[string]bool, error) {
	s, e := st.read()
	if e != nil {
		return nil, e
	}
	type helperResult struct {
		entries []claudeEntry
		ok      bool
	}
	// The agents helper lists sessions for an account, not for a member. Cache
	// only within this pass, and keep command and environment in the key: two
	// profiles can point at different clients or Claude accounts. Cwd joins the
	// key when command resolution or shell startup can depend on it.
	helperOK := map[string]bool{}
	claude := map[string]claudeEntry{}
	cache := map[string]helperResult{}
	cfg, cfgErr := s.effectiveConfig()
	for _, member := range s.Members {
		if member.Engine != config.Claude || cfgErr != nil {
			continue
		}
		command, _ := cfg.ProfileCommand(member.Profile, member.Engine)
		key, _ := json.Marshal(struct {
			Command config.Command
			Env     map[string]string
			Cwd     string
		}{command, member.Env, helperCwdKey(command, member.Env, member.Cwd)})
		result, seen := cache[string(key)]
		if !seen {
			name, args, env := command.Invocation(member.Env, "", "agents", "--json")
			out, err := process.RunStdoutEnv(member.Cwd, env, name, args...)
			if err == nil {
				result.entries, err = decodeClaudeAgents(out, command.Shell != "")
				result.ok = err == nil
			}
			cache[string(key)] = result
		}
		if result.ok {
			helperOK[member.ID] = true
			for _, entry := range result.entries {
				if entry.SessionID == member.EngineID {
					claude[member.ID] = entry
					break
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
			if c, ok := claude[m.ID]; ok {
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
