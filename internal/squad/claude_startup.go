package squad

import (
	"strings"
	"time"
)

// claudeTrustKeys recognizes only the initial workspace confirmation for the
// directory the user selected. Tool approvals and other dialogs are not handled.
func claudeTrustKeys(screen, cwd string) []string {
	if !strings.Contains(screen, "Accessing workspace:") || !strings.Contains(screen, "Yes, I trust this folder") || !strings.Contains(screen, "No, exit") {
		return nil
	}
	lines := strings.Split(screen, "\n")
	matched := false
	for i, line := range lines {
		if strings.TrimSpace(line) != "Accessing workspace:" {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			matched = strings.TrimSpace(next) == cwd
			break
		}
	}
	if !matched {
		return nil
	}
	if strings.Contains(screen, "❯ No, exit") {
		return []string{"Down"}
	}
	if strings.Contains(screen, "❯ Yes, I trust this folder") {
		return []string{"Enter"}
	}
	return nil
}

// monitorClaudeStartup belongs to the engine runner and exits when startup or
// the process finishes. Confirming the native dialog lets Claude persist its own
// project trust record without impersonating a sandbox or editing its config.
func (st *Store) monitorClaudeStartup(actor string, gen int, stopped <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var submitted bool
	var lastKey time.Time
	for {
		select {
		case <-stopped:
			return
		case <-ticker.C:
		}
		finished := false
		err := st.update(func(s *State) error {
			m := s.Members[actor]
			if !s.Active || m == nil || m.Generation != gen || m.State != MemberStateStarting {
				if submitted && m != nil && m.Generation == gen && m.EngineID != "" && s.Active {
					s.event(actor, "workspace_trusted", "Confirmed Claude workspace: "+m.Cwd)
				}
				finished = true
				return nil
			}
			if m.Pane == "" {
				return nil
			}
			screen, err := tm(s, "capture-pane", "-p", "-J", "-t", agentPane(m))
			if err != nil {
				return err
			}
			keys := claudeTrustKeys(screen, m.Cwd)
			if len(keys) == 0 || time.Since(lastKey) < time.Second {
				return nil
			}
			_, err = tm(s, append([]string{"send-keys", "-t", agentPane(m)}, keys...)...)
			if err == nil {
				lastKey = time.Now()
				submitted = keys[0] == "Enter"
			}
			return err
		})
		if finished || err != nil {
			return
		}
	}
}
