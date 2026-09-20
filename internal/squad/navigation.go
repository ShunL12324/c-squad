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

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func navigationTables(st *Store) (string, string) {
	h := sha256.Sum256([]byte(st.Dir))
	root := fmt.Sprintf("csquad-%x", h[:8])
	return root, root + "-prefix"
}

// switchHint names the member switch keys for the status bar and the sidebar
// footer. The default pair gets a friendlier label than its tmux spelling.
func switchHint(cfg config.Config) string {
	switch {
	case cfg.PreviousKey == "" || cfg.NextKey == "":
		return ""
	case cfg.PreviousKey == "M-Up" && cfg.NextKey == "M-Down":
		return "Alt+↑↓"
	default:
		return cfg.PreviousKey + "/" + cfg.NextKey
	}
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
	// Drop obsolete team bindings before copying the user's current tables.
	st.clearNavigation(s)
	if _, err = tm(s, "source-file", path); err != nil {
		return err
	}
	cmd := shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0 navigate --client '#{client_name}'"
	cfg, err := s.effectiveConfig()
	if err != nil {
		return err
	}
	// Root-table bindings are consumed before the pane sees the key, so member
	// switching works from the engine pane as well as from the sidebar, which
	// never holds focus. Configurable because tmux cannot bind the Alt encoding
	// and pass the Meta one through: see docs/usage.md.
	for _, binding := range []struct{ key, direction string }{{cfg.PreviousKey, "previous"}, {cfg.NextKey, "next"}} {
		if binding.key == "" {
			continue
		}
		if _, err = tm(s, "bind-key", "-T", root, binding.key, "run-shell", "-b", cmd+" --direction "+binding.direction); err != nil {
			return fmt.Errorf("bind %s: %w", binding.key, err)
		}
	}
	for i := 0; i < 10; i++ {
		if _, err = tm(s, "bind-key", "-T", prefix, strconv.Itoa(i), "run-shell", "-b", cmd+" --index "+strconv.Itoa(i)); err != nil {
			return err
		}
	}

	panelCmd := shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0"
	for key, view := range map[string]string{"t": "tasks"} {
		if _, err = tm(s, "bind-key", "-T", prefix, key, "run-shell", "-b", panelCmd+" ui-toggle --view "+view+" --client '#{client_name}'"); err != nil {
			return err
		}
	}
	// Every click must record its client, including tmux's SecondClick event
	// for the next press in a double click. Forward to the clicked pane explicitly, independent of old focus.
	for _, key := range []string{"MouseDown1Pane", "SecondClick1Pane", "DoubleClick1Pane", "TripleClick1Pane"} {
		binding, _ := tm(s, "list-keys", "-T", "root", key)
		_, fallback, _ := strings.Cut(binding, key)
		fallback = strings.ReplaceAll(strings.TrimSpace(fallback), `\;`, ";")
		if fallback == "" {
			fallback = "select-pane -t = ; send-keys -M -t ="
		}
		if _, err = tm(s, "bind-key", "-T", root, key, "if-shell", "-F", "-t", "=", "#{@csquad_panel}", "set-option -pF -t = @csquad_client '#{client_name}' ; select-pane -t = ; send-keys -M -t =", fallback); err != nil {
			return err
		}
	}
	// Status ranges keep button hitboxes aligned with tmux's rendered cells.
	statusClick := `if-shell -F '#{==:#{mouse_status_range},detach}' "detach-client"`
	for _, view := range []string{"tasks"} {
		action := "run-shell -b " + strconv.Quote(panelCmd+" ui-toggle --view "+view+" --client '#{client_name}'")
		statusClick = "if-shell -F " + shellQuote("#{==:#{mouse_status_range},"+view+"}") + " " + strconv.Quote(action) + " " + strconv.Quote(statusClick)
	}
	for _, key := range []string{"MouseDown1Status", "SecondClick1Status", "DoubleClick1Status", "TripleClick1Status"} {
		if _, err = tm(s, "bind-key", "-T", root, key, statusClick); err != nil {
			return err
		}
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
		switchKeys := "C-b 0–9"
		if label := switchHint(cfg); label != "" {
			switchKeys = label + " or " + switchKeys
		}
		hint := ""
		for _, button := range []struct{ id, name, key string }{{"tasks", "Tasks", "t"}, {"detach", "Detach", "d"}} {
			hint += "#[range=user|" + button.id + ",bg=colour236,fg=colour253] " + button.name + "  #[fg=colour245]C-b " + button.key + " #[norange,bg=colour234] "
		}
		for opt, val := range map[string]string{"prefix": "None", "prefix2": "None", "key-table": root, "mouse": "on", "status-style": "fg=colour252,bg=colour234", "status": "2", "status-position": "bottom", "status-left": "[" + s.ID + "] ", "status-left-length": "60", "status-format[0]": "#[fg=colour238]" + strings.Repeat("─", 500), "status-format[1]": "#[align=left,fg=colour245]  Click to select · " + switchKeys + " switch member #[align=right]" + hint} {
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
