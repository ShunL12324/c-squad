package squad

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
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
	if view == panelTasks {
		return true, width >= 150
	}
	if view == panelBoth {
		return true, width >= 150
	}
	return true, false
}

func (st *Store) panelCommand(s *State, owner, view string) string {
	return shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0 ui-panel --owner " + shellQuote(owner) + " --view " + shellQuote(view)
}

// configurePanels changes only panes owned by C Squad, never the engine pane.
func (st *Store) configurePanels() (result error) {
	unlock, err := filelock.Acquire(st.Dir, "panels", false)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := st.read()
	if err != nil {
		return err
	}
	if !s.Active {
		return nil
	}
	for _, m := range navigationMembers(s) {
		if m.Pane == "" {
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
		current, _ := tm(s, "display-message", "-p", "-t", m.Pane, "#{window_width} #{window_height}")
		if current != fields[0]+" "+fields[1] {
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
// client ever sees it. Member sessions are created detached at a fixed size, so
// tmux would otherwise reflow every pane proportionally on the switch and the
// asynchronous layout hook would only repair it a moment later.
// It reports whether it pinned the window, which the caller has to undo.
func (st *Store) fitSession(s *State, m *Member, client string) (bool, error) {
	source := clientSession(s, client)
	if source == "" {
		return false, nil
	}
	// A client resize hook can still be queued when the user clicks. Repair
	// that resize before sampling dimensions, not the transient tmux reflow.
	if err := st.configurePanels(); err != nil {
		return false, err
	}
	want := windowSize(s, source)
	if want == "" {
		return false, nil
	}
	// Capture before resizing or creating destination panes. Each member has a
	// separate tmux window, but switching members should retain this client's
	// current panel dimensions rather than that window's old/default layout.
	dimensions, err := panelDimensions(s, "="+source+":")
	if err != nil {
		return false, err
	}
	for role, d := range dimensions {
		size := d[0]
		if role == "header" {
			size = d[1]
		}
		if _, err := tm(s, "set-option", "-w", "-t", "="+source+":", "@csquad_size_"+role, strconv.Itoa(size)); err != nil {
			return false, err
		}
	}
	pinned := want != windowSize(s, m.Session)
	if pinned {
		size := strings.Fields(want)
		if _, err := tm(s, "resize-window", "-t", "="+m.Session+":", "-x", size[0], "-y", size[1]); err != nil {
			return false, err
		}
	}
	if pinned {
		if err := st.configurePanels(); err != nil {
			return pinned, err
		}
	}
	return pinned, applyPanelDimensions(s, m.Pane, dimensions)
}

func (st *Store) setPanelView(view string, toggle bool) error {
	if err := st.update(func(s *State) error {
		current := s.PanelView
		if current == "" {
			current = panelBoth
		}
		if toggle {
			switch panelView(view) {
			case panelTasks:
				switch current {
				case panelBoth:
					view = "members"
				case panelTasks:
					view = "hide"
				case panelHidden:
					view = "tasks"
				default:
					view = "both"
				}
			case panelMembers:
				switch current {
				case panelBoth:
					view = "tasks"
				case panelMembers:
					view = "hide"
				case panelHidden:
					view = "members"
				default:
					view = "both"
				}
			}
		}
		switch panelView(view) {
		case panelMembers, panelTasks, panelBoth, panelHidden:
			s.PanelView = panelView(view)
		default:
			return errors.New("view must be members, tasks, both or hide")
		}
		return nil
	}); err != nil {
		return err
	}
	return st.configurePanels()
}

func (st *Store) panelSnapshot() (teamui.Snapshot, error) {
	s, err := st.read()
	if err != nil {
		return teamui.Snapshot{}, err
	}
	cfg, err := s.effectiveConfig()
	if err != nil {
		return teamui.Snapshot{}, err
	}
	out := teamui.Snapshot{Team: s.ID, Active: s.Active, Switch: switchHint(cfg)}
	taskIDs := sortedTaskIDs(s)
	for _, m := range navigationMembers(s) {
		tasks := []string{}
		for _, id := range taskIDs {
			t := s.Tasks[id]
			if t.State != TaskPhaseDone && (t.Owner == m.ID || slices.Contains(t.Participants, m.ID)) {
				tasks = append(tasks, id)
			}
		}
		card := teamui.Member{ID: m.ID, Engine: string(m.Engine), State: strings.ReplaceAll(string(m.State), "_", " "), Color: strings.TrimPrefix(m.Color.StyleValue(), "colour"), Cwd: displayDirectory(m.Cwd), Tasks: strings.Join(tasks, ", ")}
		// Resolve from the raw cwd, never from displayDirectory's abbreviation and
		// never from the task's workspace: in a cross-repository task the member
		// is on another repository's branch entirely.
		if state, ok := memberGit(m.Cwd); ok {
			card.Branch, card.Commit, card.Worktree = state.Branch, state.Commit, state.Worktree
		}
		out.Members = append(out.Members, card)
	}
	for _, id := range taskIDs {
		t := s.Tasks[id]
		color := "252"
		if owner := s.Members[t.Owner]; owner != nil {
			color = strings.TrimPrefix(owner.Color.StyleValue(), "colour")
		}
		workspace := t.Workspace
		if workspace == "" && s.Members[t.Owner] != nil {
			workspace = s.Members[t.Owner].Cwd
		}
		detail := fmt.Sprintf("With: %s\n\nGoal\n%s\n\nAcceptance\n%s\n\nLatest update\n%s\n\nWorkspace\n%s\n\nUpdated: %s", strings.Join(t.Participants, ", "), t.Description, t.Acceptance, t.Progress, workspace, t.Updated)
		if len(t.Blockers) > 0 {
			detail += "\n\nBlocked\n" + strings.Join(t.Blockers, "\n")
		}
		milestones := make([]teamui.Milestone, 0, len(t.Milestones))
		for _, ms := range t.Milestones {
			milestones = append(milestones, teamui.Milestone{Name: ms.Name, State: string(ms.State), Gate: ms.Gate})
		}
		for _, e := range t.Evidence {
			result := "failed"
			if e.Passed {
				result = "passed"
			}
			detail += fmt.Sprintf("\n\n%s · %s · %s\n%s", e.Member, e.Kind, result, e.Summary)
		}
		if t.Candidate != "" {
			detail += "\n\nCandidate: " + t.Candidate
		}
		if t.MergeCommit != "" {
			detail += "\n\nMerged: " + t.MergeCommit
		}
		// A task closed on outside evidence must never render like a merged one.
		note := ""
		if t.ExternalClosure != nil {
			note = "Closed externally · not merged · " + short(t.ExternalClosure.SHA)
			detail += "\n\n" + strings.Join(describeExternalClosure(t.ExternalClosure), "\n")
		}
		confirmation := ""
		canConfirm := t.State == TaskPhaseDone && t.UserConfirmation == nil
		if canConfirm {
			confirmation = "Awaiting user confirmation"
		}
		if t.UserConfirmation != nil {
			confirmation = "User confirmed"
			detail += "\n\nUser confirmation: " + t.UserConfirmation.At + " (" + t.UserConfirmation.Actor + ")"
		}
		out.Tasks = append(out.Tasks, teamui.Task{ID: t.ID, Title: t.Title, State: strings.ReplaceAll(string(t.State), "_", " "), Owner: t.Owner, Color: color, Progress: t.Progress, Detail: detail, Note: note, Confirmation: confirmation, CanConfirm: canConfirm, Milestones: milestones, Brief: briefFor(s, t.ID)})
	}
	return out, nil
}
func sortedTaskIDs(s *State) []string {
	ids := make([]string, 0, len(s.Tasks))
	for id := range s.Tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.Tasks[ids[i]], s.Tasks[ids[j]]
		if (a.State == TaskPhaseDone) != (b.State == TaskPhaseDone) {
			return a.State != TaskPhaseDone
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (st *Store) runPanel(owner, view string, popup bool) error {
	if view != "members" && view != "tasks" && view != "header" {
		return errors.New("panel must be members or tasks")
	}
	pane := os.Getenv("TMUX_PANE")
	return teamui.Run(view, owner, st.panelSnapshot, func(a teamui.Action) (string, error) {
		s, err := st.read()
		if err != nil {
			return "", err
		}
		if a.Kind == "close" {
			if popup {
				return "", nil
			}
			return "", st.setPanelView(view, true)
		}
		// The panel runs as master from argv C Squad builds itself, which is the
		// identity the request is made under; the message records the user as
		// its origin. It queues a question and touches no task state.
		if a.Kind == "confirm" {
			return st.confirmTask(a.Task)
		}
		if a.Kind == "brief" {
			return briefRequest(st, "master", a.Task)
		}
		m, err := s.member(a.Member)
		if err != nil {
			return "", err
		}
		client, _ := tm(s, "show-options", "-pv", "-t", pane, "@csquad_client")
		// A clicked panel records its originating client. Keyboard-only navigation
		// is unambiguous when exactly one client is attached to this member session.
		host, err := s.member(owner)
		if err != nil {
			return "", err
		}
		rows, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}")
		if err != nil {
			return "", err
		}
		candidates := []string{}
		for _, line := range strings.Split(rows, "\n") {
			name, session, ok := strings.Cut(line, "\t")
			if ok && session == host.Session {
				candidates = append(candidates, name)
			}
		}
		if !slices.Contains(candidates, client) {
			if len(candidates) != 1 {
				return "", errors.New("click this panel to select a client")
			}
			client = candidates[0]
		}
		return "", st.switchMember(s, m, client)
	})
}

func (st *Store) openUI(o options, toggle bool) error {
	view := o["view"]
	if view == "" {
		view = "both"
	}
	if toggle && view == "tasks" {
		s, err := st.read()
		if err != nil {
			return err
		}
		opened, err := st.compactPanelPopup(s, view, o["client"])
		if err != nil || opened {
			return err
		}
	}
	if err := st.setPanelView(view, toggle); err != nil {
		return err
	}
	// A Tasks toggle already checked compact mode before changing the layout.
	// Do not check again after that asynchronous work: a terminal shrink in
	// between would unexpectedly open a popup and intercept subsequent resize
	// events intended for the normal panel layout.
	if view == "hide" || toggle && view == "tasks" {
		return nil
	}
	s, err := st.read()
	if err != nil {
		return err
	}
	_, err = st.compactPanelPopup(s, view, o["client"])
	return err
}

// compactPanelPopup keeps member navigation visible when the task board cannot
// fit alongside the engine. It never changes the saved wide-screen layout.
func (st *Store) compactPanelPopup(s *State, view, client string) (bool, error) {
	rows, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}\t#{client_width}")
	if err != nil {
		return false, err
	}
	for _, row := range strings.Split(rows, "\n") {
		f := strings.Split(row, "\t")
		if len(f) != 3 || client != "" && client != f[0] {
			continue
		}
		for _, m := range navigationMembers(s) {
			if m.Session != f[1] {
				continue
			}
			width, _ := strconv.Atoi(f[2])
			members, tasks := panelVisibility(panelBoth, width)
			popupView := view
			if popupView == "both" {
				popupView = "tasks"
			}
			if popupView == "tasks" && tasks || popupView == "members" && members {
				return false, nil
			}
			_, err = tm(s, "display-popup", "-c", f[0], "-E", "-w", "95%", "-h", "90%", st.panelCommand(s, m.ID, popupView)+" --popup")
			return true, err
		}
	}
	return false, nil
}

// displayDirectory abbreviates only the user's home, preserving the actual cwd.
func displayDirectory(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

// panelDimensions uses roles rather than pane IDs, which differ by session.
func panelDimensions(s *State, target string) (map[string][2]int, error) {
	rows, err := tm(s, "list-panes", "-t", target, "-F", "#{@csquad_panel} #{pane_width} #{pane_height}")
	out := map[string][2]int{}
	for _, row := range strings.Split(rows, "\n") {
		f := strings.Fields(row)
		if len(f) != 3 {
			continue
		}
		w, _ := strconv.Atoi(f[1])
		h, _ := strconv.Atoi(f[2])
		out[f[0]] = [2]int{w, h}
	}
	return out, err
}

func applyPanelDimensions(s *State, target string, dimensions map[string][2]int) error {
	rows, err := tm(s, "list-panes", "-t", target, "-F", "#{pane_id} #{@csquad_panel}")
	if err != nil {
		return err
	}
	panes := map[string]string{}
	for _, row := range strings.Split(rows, "\n") {
		f := strings.Fields(row)
		if len(f) == 2 {
			panes[f[1]] = f[0]
		}
	}
	// tmux clamps sizes to the available space. Hidden responsive panels stay
	// hidden, so small clients retain usable engine space.
	for _, role := range []string{"header", "members", "tasks"} {
		d, ok := dimensions[role]
		pane := panes[role]
		if !ok || pane == "" {
			continue
		}
		axis, size := "-x", d[0]
		if role == "header" {
			axis, size = "-y", d[1]
		}
		if _, err := tm(s, "set-option", "-w", "-t", target, "@csquad_size_"+role, strconv.Itoa(size)); err != nil {
			return err
		}
		if _, err := tm(s, "resize-pane", "-t", pane, axis, strconv.Itoa(size)); err != nil {
			return err
		}
	}
	return nil
}

// rememberPanelLayout runs after an explicit resize-pane (including border
// drags). Ignore layout repair during a window resize: those dimensions are a
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
