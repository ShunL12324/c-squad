package squad

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func navigationTables(st *Store) (string, string) {
	h := sha256.Sum256([]byte(st.Dir))
	root := fmt.Sprintf("csquad-%x", h[:8])
	return root, root + "-prefix"
}
func navigationMembers(s *State) []*Member {
	out := []*Member{}
	for _, m := range s.Members {
		if m.State != MemberStateRemoved && m.State != MemberStateStopped && m.State != MemberStateStopping {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == "master" {
			return true
		}
		if out[j].ID == "master" {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Dedicated session key tables preserve the user's server-wide bindings.
func (st *Store) configureNavigation() error {
	unlock, err := filelock.Acquire(st.Dir, "navigation", false)
	if err != nil {
		return err
	}
	defer unlock()
	// Persist defaults once so refresh, restart and recovery retain member colors.
	if err = st.update(func(s *State) error {
		for _, m := range s.Members {
			if m.Color == "" {
				m.Color = tmux.RandomColor()
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s, err := st.read()
	if err != nil {
		return err
	}
	version, err := tm(s, "display-message", "-p", "#{version}")
	if err != nil {
		return err
	}
	var major, minor int
	_, _ = fmt.Sscanf(version, "%d.%d", &major, &minor)
	clickable := major > 3 || major == 3 && minor >= 4
	root, prefix := navigationTables(st)
	var source strings.Builder
	for _, table := range []struct{ from, to string }{{"root", root}, {"prefix", prefix}} {
		keys, e := tm(s, "list-keys", "-T", table.from)
		if e != nil {
			return e
		}
		re := regexp.MustCompile(`-T\s+` + table.from + `\s+`)
		source.WriteString(re.ReplaceAllString(keys, "-T "+table.to+" "))
		source.WriteByte('\n')
	}
	dir := filepath.Join(st.Dir, "runtime")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "navigation.keys")
	if err = os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		return err
	}
	if _, err = tm(s, "source-file", path); err != nil {
		return err
	}
	cmd := shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0 navigate --client '#{client_name}'"
	for key, dir := range map[string]string{"M-Left": "previous", "M-Right": "next"} {
		if _, err = tm(s, "bind-key", "-T", root, key, "run-shell", "-b", cmd+" --direction "+dir); err != nil {
			return err
		}
	}
	for i := 0; i < 10; i++ {
		if _, err = tm(s, "bind-key", "-T", prefix, strconv.Itoa(i), "run-shell", "-b", cmd+" --index "+strconv.Itoa(i)); err != nil {
			return err
		}
	}
	// Only member labels carry numeric ranges. Ignore clicks on empty space or
	// shortcut hints rather than treating them as a window-selection request.
	if clickable {
		if _, err = tm(s, "bind-key", "-T", root, "MouseDown1Status", "if-shell", "-F",
			"#{m/r:^[0-9]+$,#{mouse_status_range}}", "run-shell -b "+shellQuote(cmd+" --index '#{mouse_status_range}'")); err != nil {
			return err
		}
	}

	panelCmd := shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0"
	for key, view := range map[string]string{"b": "members", "t": "tasks"} {
		if _, err = tm(s, "bind-key", "-T", prefix, key, "run-shell", "-b", panelCmd+" ui-toggle --view "+view+" --client '#{client_name}'"); err != nil {
			return err
		}
	}
	// Record the mouse's originating client before forwarding it into a panel.
	if _, err = tm(s, "bind-key", "-T", root, "MouseDown1Pane", "if-shell", "-F", "-t", "=", "#{@csquad_panel}", "set-option -pF -t = @csquad_client '#{client_name}' ; select-pane -t = ; send-keys -M", "select-pane -t = ; send-keys -M"); err != nil {
		return err
	}
	members := navigationMembers(s)
	for _, m := range members {
		target := "=" + m.Session
		if _, e := tm(s, "has-session", "-t", target); e != nil {
			continue
		}
		// Prefix processing normally uses the global prefix table. Route it through
		// a private table so numeric member shortcuts do not affect unrelated teams.
		for _, opt := range []string{"prefix", "prefix2"} {
			key, keyErr := tm(s, "show-options", "-v", "-t", target, "@csquad_"+opt)
			if keyErr != nil {
				key = ""
			}
			if key == "" {
				key, _ = tm(s, "show-options", "-Av", "-t", target, opt)
				if key == "" {
					key = "None"
				}
				if _, err = tm(s, "set-option", "-t", target, "@csquad_"+opt, key); err != nil {
					return err
				}
			}
			if key != "None" {
				if _, err = tm(s, "bind-key", "-T", root, key, "switch-client", "-T", prefix); err != nil {
					return err
				}
			}
		}
		// Always offer the documented default prefix as well.
		if _, err = tm(s, "bind-key", "-T", root, "C-b", "switch-client", "-T", prefix); err != nil {
			return err
		}

		if m.Pane != "" {
			for _, hook := range []string{"client-attached", "client-session-changed", "client-resized"} {
				if _, err = tm(s, "set-hook", "-t", target, hook+"[914]", "run-shell -b "+shellQuote(panelCmd+" ui-layout")); err != nil {
					return err
				}
			}
		}
		labels := []string{}
		for i, other := range members {
			label := fmt.Sprintf(" %d:%s ", i, other.ID)
			style := "fg=" + other.Color.StyleValue() + ",bg=colour234"
			if other.ID == m.ID {
				style = "fg=colour234,bg=" + other.Color.StyleValue() + ",bold"
			}
			if clickable {
				labels = append(labels, fmt.Sprintf("#[range=user|%d,%s]%s#[norange,default]", i, style, label))
			} else {
				labels = append(labels, fmt.Sprintf("#[%s]%s#[default]", style, label))
			}
		}
		hint := "C-b b:members t:tasks "
		if clickable {
			hint = "Click member · C-b b/t panels "
		}
		for opt, val := range map[string]string{"prefix": "None", "prefix2": "None", "key-table": root, "mouse": "on", "status-style": "fg=colour252,bg=colour234", "status": "on", "status-left": "[" + s.ID + "] ", "status-left-length": "60", "status-format[0]": "#[align=left]" + strings.Join(labels, "") + " #[align=right]" + hint} {
			if _, err = tm(s, "set-option", "-t", target, opt, val); err != nil {
				return err
			}
		}
	}
	return st.configurePanels()
}
func (st *Store) navigate(client, direction, index string) error {
	s, err := st.read()
	if err != nil {
		return err
	}
	if client == "" {
		return fmt.Errorf("--client required")
	}
	clients, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}")
	if err != nil {
		return err
	}
	current := ""
	for _, line := range strings.Split(clients, "\n") {
		name, session, ok := strings.Cut(line, "\t")
		if ok && name == client {
			current = session
			break
		}
	}
	members := navigationMembers(s)
	live := []*Member{}
	at := -1
	for _, m := range members {
		if _, e := tm(s, "has-session", "-t", "="+m.Session); e == nil {
			if m.Session == current {
				at = len(live)
			}
			live = append(live, m)
		}
	}
	if at < 0 {
		return fmt.Errorf("client is not attached to this team")
	}
	if index != "" {
		n, e := strconv.Atoi(index)
		if e != nil || n < 0 || n >= len(members) {
			return fmt.Errorf("invalid member index")
		}
		_, err = tm(s, "switch-client", "-c", client, "-t", "="+members[n].Session)
		return err
	}
	delta := 1
	if direction == "previous" {
		delta = -1
	} else if direction != "next" {
		return fmt.Errorf("invalid direction")
	}
	target := live[(at+delta+len(live))%len(live)]
	_, err = tm(s, "switch-client", "-c", client, "-t", "="+target.Session)
	return err
}
func (st *Store) clearNavigation(s *State) {
	root, prefix := navigationTables(st)
	// Tables may already be absent after server exit; cleanup is idempotent.
	_, _ = tm(s, "unbind-key", "-a", "-T", root)
	// Tables may already be absent after server exit; cleanup is idempotent.
	_, _ = tm(s, "unbind-key", "-a", "-T", prefix)
}
