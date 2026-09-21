package squad

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/teamui"
)

type panelView string

const (
	panelMembers panelView = "members"
	panelTasks   panelView = "tasks"
	panelBoth    panelView = "both"
	panelHidden  panelView = "hide"
)

func panelVisibility(view panelView, width int) (bool, bool) {
	if view == "" {
		view = panelBoth
	}
	if width < 90 || view == panelHidden {
		return false, false
	}
	if view == panelTasks || view == panelBoth {
		return true, width >= 150
	}
	return true, false
}

func (st *Store) panelCommand(s *State, owner, view string) string {
	return shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0 ui-panel --owner " + shellQuote(owner) + " --view " + shellQuote(view)
}

// configurePanels changes only panes owned by C Squad, never the engine pane.
func (st *Store) configurePanels() error { return st.configureSelectedPanels() }

func (st *Store) configureSelectedPanels(owners ...string) (result error) {
	unlock, err := filelock.Acquire(st.Dir, "panels", false)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := st.read()
	if err != nil {
		return err
	}
	return st.configurePanelState(s, owners...)
}

// The caller holds the panels lock, including when fitting a destination.
func (st *Store) configurePanelState(s *State, owners ...string) (result error) {
	if !s.Active {
		return nil
	}
	for _, m := range navigationMembers(s) {
		if len(owners) > 0 && !slices.Contains(owners, m.ID) {
			continue
		}
		if m.Pane == "" {
			continue
		}
		current, e := readPanelGeometry(s, m.Pane)
		if e != nil {
			continue
		}
		if current.ready(s.PanelView) {
			if err := savePanelDimensions(s, m.Pane, current.dimensions); err != nil {
				return err
			}
			continue
		}
		for option, value := range map[string]string{
			"pane-border-status":       "off",
			"pane-border-style":        "fg=colour238,bg=colour234",
			"pane-active-border-style": "fg=colour238,bg=colour234",
		} {
			if _, err := tm(s, "set-option", "-w", "-t", m.Pane, option, value); err != nil {
				return err
			}
		}
		geometry, e := tm(s, "display-message", "-p", "-t", m.Pane, "#{window_width} #{window_height} #{pane_dead}")
		if e != nil {
			continue
		}
		fields := strings.Fields(geometry)
		if len(fields) != 3 || fields[2] != "0" {
			continue
		}
		// Capture a native divider drag before applying any saved preferences.
		// A changed window size instead denotes tmux's automatic reflow.
		if current.size == current.saved {
			if e := savePanelDimensions(s, m.Pane, current.dimensions); e != nil {
				return e
			}
		}
		width, _ := strconv.Atoi(fields[0])
		height, _ := strconv.Atoi(fields[1])
		if _, e = tm(s, "set-option", "-w", "-t", m.Pane, "@csquad_layout_active", "1"); e != nil {
			return e
		}
		defer func() {
			_, err := tm(s, "set-option", "-w", "-t", m.Pane, "@csquad_layout_active", "0")
			if result == nil {
				result = err
			}
		}()
		members, tasks := panelVisibility(s.PanelView, width)
		if height < 12 {
			members = false
			tasks = false
		}
		panes, e := tm(s, "list-panes", "-t", m.Pane, "-F", "#{pane_id}\t#{@csquad_panel}\t#{pane_dead}")
		if e != nil {
			return e
		}
		existing := map[string]string{}
		for _, line := range strings.Split(panes, "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 3 || f[0] == m.Pane || f[1] == "" {
				continue
			}
			want := f[1] == "members" && members || f[1] == "tasks" && tasks || f[1] == "header" && (members || tasks)
			if !want || f[2] == "1" {
				if _, e = tm(s, "kill-pane", "-t", f[0]); e != nil {
					return e
				}
			} else {
				if _, e = tm(s, "set-option", "-p", "-t", f[0], "window-style", "bg=colour"+teamui.CanvasColor); e != nil {
					return e
				}
				saved, _ := tm(s, "show-options", "-wv", "-t", m.Pane, "@csquad_size_"+f[1])
				if n, err := strconv.Atoi(saved); err == nil && n > 0 {
					axis := "-x"
					if f[1] == "header" {
						axis = "-y"
					}
					format := "#{pane_width}"
					if f[1] == "header" {
						format = "#{pane_height}"
					}
					actual, e := tm(s, "display-message", "-p", "-t", f[0], format)
					if e != nil {
						return e
					}
					if actual != saved {
						if _, e = tm(s, "resize-pane", "-t", f[0], axis, saved); e != nil {
							return e
						}
					}
				}
				existing[f[1]] = f[0]
			}
		}
		for _, view := range []string{"members", "tasks", "header"} {
			want := view == "members" && members || view == "tasks" && tasks || view == "header" && (members || tasks)
			if !want || existing[view] != "" {
				continue
			}
			args := []string{"split-window", "-d", "-h", "-t", m.Pane, "-l", "40", "-P", "-F", "#{pane_id}"}
			if view == "members" {
				args = append(args, "-b")
				args[6] = "28"
			}
			if view == "header" {
				args = []string{"split-window", "-d", "-v", "-f", "-b", "-t", m.Pane, "-l", "3", "-P", "-F", "#{pane_id}"}
			}
			saved, _ := tm(s, "show-options", "-wv", "-t", m.Pane, "@csquad_size_"+view)
			if n, err := strconv.Atoi(saved); err == nil && n > 0 {
				if view == "header" {
					args[8] = strconv.Itoa(min(n, height-2))
				} else {
					space, err := tm(s, "display-message", "-p", "-t", m.Pane, "#{pane_width}")
					if err != nil {
						return err
					}
					available, _ := strconv.Atoi(space)
					if available < 3 {
						continue
					}
					args[6] = strconv.Itoa(min(n, available-2))
				}
			} else {
				saved = "40"
				if view == "members" {
					saved = "28"
				}
				if view == "header" {
					saved = "3"
				}
				if _, e = tm(s, "set-option", "-w", "-t", m.Pane, "@csquad_size_"+view, saved); e != nil {
					return e
				}
			}
			args = append(args, st.panelCommand(s, m.ID, view))
			pane, e := tm(s, args...)
			if e != nil {
				return e
			}
			if _, e = tm(s, "set-option", "-p", "-t", pane, "window-style", "bg=colour"+teamui.CanvasColor); e != nil {
				return e
			}
			if _, e = tm(s, "set-option", "-p", "-t", pane, "@csquad_panel", view); e != nil {
				return e
			}
			if _, e = tm(s, "set-option", "-p", "-t", pane, "@csquad_owner", m.ID); e != nil {
				return e
			}
			if _, e = tm(s, "set-option", "-p", "-t", pane, "remain-on-exit", "off"); e != nil {
				return e
			}

			existing[view] = pane
		}

		// A terminal can resize while a layout pass is running. Never save its
		// transient reflow as the user's preferred size or mark that resize handled.
		currentSize, _ := tm(s, "display-message", "-p", "-t", m.Pane, "#{window_width} #{window_height}")
		if currentSize != fields[0]+" "+fields[1] {
			continue
		}

		if _, e = tm(s, "set-option", "-w", "-t", m.Pane, "@csquad_geometry", fields[0]+" "+fields[1]); e != nil {
			return e
		}
	}
	return nil
}

// windowSize reports the geometry a session is rendered at. display-message
// resolves #{window_*} against its target rather than against -c, so the
// session has to be named explicitly even when a client is known.
func windowSize(s *State, session string) string {
	out, err := tm(s, "display-message", "-p", "-t", "="+session+":", "#{window_width} #{window_height}")
	if err != nil || len(strings.Fields(out)) != 2 {
		return ""
	}
	return out
}

func clientSession(s *State, client string) string {
	rows, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}")
	if err != nil {
		return ""
	}
	for _, row := range strings.Split(rows, "\n") {
		if name, session, ok := strings.Cut(row, "\t"); ok && name == client {
			return session
		}
	}
	return ""
}

// teamWindowSize births a member session at the geometry the team is actually
// displayed at. The previous fixed size forced tmux to reflow every pane the
// first time a client switched into a newly created member.
func teamWindowSize(s *State) (string, string) {
	rows, err := tm(s, "list-clients", "-F", "#{session_name}")
	if err == nil {
		for _, m := range navigationMembers(s) {
			for _, session := range strings.Split(rows, "\n") {
				if session == "" || session != m.Session {
					continue
				}
				if size := strings.Fields(windowSize(s, session)); len(size) == 2 {
					return size[0], size[1]
				}
			}
		}
	}
	return "140", "42"
}

// fitSession lays the destination out at the switching client's size before the
// client ever sees it. Destination geometry can differ after viewport changes
// or fallback sizing; fitting before the switch avoids visible reflow while
// waiting for the asynchronous layout hook.
// It reports whether it pinned the window, which the caller has to undo.
func (st *Store) fitSession(s *State, m *Member, client string) (bool, error) {
	unlock, err := filelock.Acquire(st.Dir, "panels", false)
	if err != nil {
		return false, err
	}
	defer unlock()
	source := clientSession(s, client)
	if source == "" {
		return false, nil
	}
	sourceTarget := "=" + source + ":"
	g, err := readPanelGeometry(s, sourceTarget)
	if err != nil {
		return false, err
	}
	if !g.ready(s.PanelView) {
		for _, host := range s.Members {
			if host.Session == source {
				if err := st.configurePanelState(s, host.ID); err != nil {
					return false, err
				}
				break
			}
		}
		g, err = readPanelGeometry(s, sourceTarget)
		if err != nil {
			return false, err
		}
	}
	if g.size == "" {
		return false, nil
	}
	if err := savePanelDimensions(s, sourceTarget, g.dimensions); err != nil {
		return false, err
	}
	destination, err := readPanelGeometry(s, m.Pane)
	if err != nil {
		return false, err
	}
	pinned := g.size != destination.size
	if pinned {
		size := strings.Fields(g.size)
		if _, err := tm(s, "resize-window", "-t", "="+m.Session+":", "-x", size[0], "-y", size[1]); err != nil {
			return false, err
		}
	}
	if pinned || !destination.ready(s.PanelView) {
		if err := st.configurePanelState(s, m.ID); err != nil {
			return pinned, err
		}
	}
	return pinned, applyPanelDimensions(s, m.Pane, g.dimensions)
}

func applyPanelDimensions(s *State, target string, dimensions map[string][2]int) error {
	g, err := readPanelGeometry(s, target)
	if err != nil {
		return err
	}
	args := []string{"set-option", "-w", "-t", target, "@csquad_layout_active", "1"}
	for _, role := range []string{"header", "members", "tasks"} {
		d, ok := dimensions[role]
		pane := g.panes[role]
		if !ok || pane == "" {
			continue
		}
		axis, n, current := "-x", d[0], g.dimensions[role][0]
		if role == "header" {
			axis, n, current = "-y", d[1], g.dimensions[role][1]
		}
		args = appendTmCommand(args, "set-option", "-w", "-t", target, "@csquad_size_"+role, strconv.Itoa(n))
		if n != current {
			args = appendTmCommand(args, "resize-pane", "-t", pane, axis, strconv.Itoa(n))
		}
	}
	args = appendTmCommand(args, "set-option", "-w", "-t", target, "@csquad_layout_active", "0")
	_, err = tm(s, args...)
	if err != nil {
		_, _ = tm(s, "set-option", "-w", "-t", target, "@csquad_layout_active", "0")
	}
	return err
}

// rememberPanelLayout runs after an explicit resize-pane. Native border drags
// only run this hook at their start; the release binding saves final dimensions
// directly in tmux, and fitSession samples them again before navigation.
// Ignore layout repair during a window resize: those dimensions are a
// transient reflow, not a new user preference. No panels lock is taken because
// configurePanels can itself trigger this synchronous tmux hook.
func (st *Store) rememberPanelLayout(owner string) error {
	s, err := st.read()
	if err != nil {
		return err
	}
	m, err := s.member(owner)
	if err != nil {
		return err
	}
	rows, err := tm(s, "list-panes", "-t", m.Pane, "-F", "#{@csquad_panel}|#{pane_width}|#{pane_height}|#{window_width} #{window_height}|#{@csquad_geometry}|#{@csquad_layout_active}")
	if err != nil {
		return err
	}
	for _, row := range strings.Split(rows, "\n") {
		f := strings.Split(row, "|")
		if len(f) != 6 || f[0] == "" || f[3] != f[4] || f[5] == "1" {
			continue
		}
		size := f[1]
		if f[0] == "header" {
			size = f[2]
		}
		if _, err := tm(s, "set-option", "-w", "-t", m.Pane, "@csquad_size_"+f[0], size); err != nil {
			return err
		}
	}
	return nil
}

// switchMember is shared by pointer navigation, prefix indices and next/previous.
func (st *Store) switchMember(s *State, m *Member, client string) (result error) {
	pinned, err := st.fitSession(s, m, client)
	if pinned {
		// Also release a pin on any failure before or during the switch.
		defer func() {
			_, err := tm(s, "set-option", "-w", "-t", "="+m.Session+":", "window-size", "latest")
			if result == nil {
				result = err
			}
		}()
	}
	if err != nil {
		return err
	}
	if _, err := tm(s, "select-pane", "-t", agentPane(m)); err != nil {
		return err
	}
	_, err = tm(s, "switch-client", "-c", client, "-t", "="+m.Session)
	return err
}
