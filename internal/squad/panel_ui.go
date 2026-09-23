package squad

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/teamui"
)

func (st *Store) setPanelView(view string, toggle bool) error {
	if err := st.update(func(s *State) error {
		current := s.PanelView
		if current == "" {
			current = panelBoth
		}
		if toggle {
			view = string(togglePanelView(current, panelView(view)))
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
		// Brief sends native user input to master without changing the task or
		// writing a team message.
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

// togglePanelView is the view a Tasks or Members toggle leads to. The member
// sidebar stays visible whenever panels are shown, so closing Tasks from either
// view that includes the board leaves the sidebar rather than hiding everything.
func togglePanelView(current, toggled panelView) panelView {
	switch toggled {
	case panelTasks:
		switch current {
		case panelBoth, panelTasks:
			return panelMembers
		case panelHidden:
			return panelTasks
		default:
			return panelBoth
		}
	case panelMembers:
		switch current {
		case panelBoth:
			return panelTasks
		case panelMembers:
			return panelHidden
		case panelHidden:
			return panelMembers
		default:
			return panelBoth
		}
	}
	return toggled
}
