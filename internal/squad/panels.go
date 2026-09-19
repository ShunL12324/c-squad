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
		return false, true
	}
	if view == panelBoth {
		return width >= 150, true
	}
	return true, false
}

func (st *Store) panelCommand(s *State, owner, view string) string {
	return shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member master --generation 0 ui-panel --owner " + shellQuote(owner) + " --view " + shellQuote(view)
}

// configurePanels changes only panes owned by C Squad, never the engine pane.
func (st *Store) configurePanels() error {
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
			want := f[1] == "members" && members || f[1] == "tasks" && tasks
			if !want || f[2] == "1" {
				if _, e = tm(s, "kill-pane", "-t", f[0]); e != nil {
					return e
				}
			} else {
				existing[f[1]] = f[0]
			}
		}
		for _, view := range []string{"members", "tasks"} {
			want := view == "members" && members || view == "tasks" && tasks
			if !want || existing[view] != "" {
				continue
			}
			args := []string{"split-window", "-d", "-h", "-t", m.Pane, "-l", "36", "-P", "-F", "#{pane_id}"}
			if view == "members" {
				args = append(args, "-b")
				args[6] = "24"
			}
			args = append(args, st.panelCommand(s, m.ID, view))
			pane, e := tm(s, args...)
			if e != nil {
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
		}
	}
	return nil
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
	out := teamui.Snapshot{Team: s.ID, Active: s.Active}
	taskIDs := sortedTaskIDs(s)
	for _, m := range navigationMembers(s) {
		tasks := []string{}
		for _, id := range taskIDs {
			t := s.Tasks[id]
			if t.State != TaskPhaseDone && (t.Owner == m.ID || slices.Contains(t.Participants, m.ID)) {
				tasks = append(tasks, id)
			}
		}
		out.Members = append(out.Members, teamui.Member{ID: m.ID, Engine: string(m.Engine), State: strings.ReplaceAll(string(m.State), "_", " "), Color: strings.TrimPrefix(m.Color.StyleValue(), "colour"), Tasks: strings.Join(tasks, ", ")})
	}
	for _, id := range taskIDs {
		t := s.Tasks[id]
		color := "252"
		if owner := s.Members[t.Owner]; owner != nil {
			color = strings.TrimPrefix(owner.Color.StyleValue(), "colour")
		}
		detail := fmt.Sprintf("With: %s\n\nGoal\n%s\n\nAcceptance\n%s\n\nUpdated: %s", strings.Join(t.Participants, ", "), t.Description, t.Acceptance, t.Updated)
		if len(t.Blockers) > 0 {
			detail += "\n\nBlocked\n" + strings.Join(t.Blockers, "\n")
		}
		for _, ms := range t.Milestones {
			detail += fmt.Sprintf("\nCheckpoint: %s · %s", ms.Name, ms.State)
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
		out.Tasks = append(out.Tasks, teamui.Task{ID: t.ID, Title: t.Title, State: strings.ReplaceAll(string(t.State), "_", " "), Owner: t.Owner, Color: color, Progress: t.Progress, Detail: detail})
	}
	ids := []string{}
	for id, q := range s.Questions {
		if q.State == QuestionStateOpen {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		q := s.Questions[id]
		out.Requests = append(out.Requests, fmt.Sprintf("%s · %s · %s\n%s\nAsk Master to handle this request.", id, q.Member, q.Task, q.Text))
	}
	for i := len(s.Events) - 1; i >= max(0, len(s.Events)-30); i-- {
		e := s.Events[i]
		out.Activity = append(out.Activity, fmt.Sprintf("%s · %s\n%s · %s", e.At, e.Member, e.Kind, e.Text))
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
	if view != "members" && view != "tasks" {
		return errors.New("panel must be members or tasks")
	}
	pane := os.Getenv("TMUX_PANE")
	return teamui.Run(view, owner, st.panelSnapshot, func(a teamui.Action) error {
		s, err := st.read()
		if err != nil {
			return err
		}
		if a.Kind == "close" {
			if popup {
				return nil
			}
			return st.setPanelView(view, true)
		}
		m, err := s.member(a.Member)
		if err != nil {
			return err
		}
		client, _ := tm(s, "show-options", "-pv", "-t", pane, "@csquad_client")
		// A clicked panel records its originating client. Keyboard-only navigation
		// is unambiguous when exactly one client is attached to this member session.
		host, err := s.member(owner)
		if err != nil {
			return err
		}
		rows, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}")
		if err != nil {
			return err
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
				return errors.New("click this panel to select a client")
			}
			client = candidates[0]
		}
		if _, err = tm(s, "select-pane", "-t", agentPane(m)); err != nil {
			return err
		}
		_, err = tm(s, "switch-client", "-c", client, "-t", "="+m.Session)
		return err
	})
}

func (st *Store) openUI(o options, toggle bool) error {
	view := o["view"]
	if view == "" {
		view = "both"
	}
	if err := st.setPanelView(view, toggle); err != nil {
		return err
	}
	if view == "hide" {
		return nil
	}
	s, err := st.read()
	if err != nil {
		return err
	}
	client := o["client"]
	rows, err := tm(s, "list-clients", "-F", "#{client_name}\t#{session_name}\t#{client_width}")
	if err != nil {
		return err
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
			if width >= 90 {
				return nil
			}
			popupView := view
			if view == "both" {
				popupView = "tasks"
			}
			_, err = tm(s, "display-popup", "-c", f[0], "-E", "-w", "95%", "-h", "90%", st.panelCommand(s, m.ID, popupView)+" --popup")
			return err
		}
	}
	return nil
}
