package squad

import (
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/tmux"
)

type panelGeometry struct {
	size, saved string
	panes       map[string]string
	dimensions  map[string][2]int
	dead        bool
}

func readPanelGeometry(s *State, target string) (panelGeometry, error) {
	rows, err := tm(s, "list-panes", "-t", target, "-F", "#{pane_id}|#{@csquad_panel}|#{pane_width}|#{pane_height}|#{window_width} #{window_height}|#{@csquad_geometry}|#{pane_dead}")
	g := panelGeometry{panes: map[string]string{}, dimensions: map[string][2]int{}}
	for _, row := range strings.Split(rows, "\n") {
		f := strings.Split(row, "|")
		if len(f) != 7 {
			continue
		}
		g.size, g.saved = f[4], f[5]
		if f[1] == "" {
			continue
		}
		w, _ := strconv.Atoi(f[2])
		h, _ := strconv.Atoi(f[3])
		g.panes[f[1]] = f[0]
		g.dimensions[f[1]] = [2]int{w, h}
		g.dead = g.dead || f[6] == "1"
	}
	return g, err
}

// ready distinguishes terminal reflow from a user moving a divider.
// Native tmux mouse motions after drag start do not run after-resize-pane.
func (g panelGeometry) ready(view panelView) bool {
	size := strings.Fields(g.size)
	if len(size) != 2 || g.size != g.saved || g.dead {
		return false
	}
	w, _ := strconv.Atoi(size[0])
	h, _ := strconv.Atoi(size[1])
	members, tasks := panelVisibility(view, w)
	if h < 12 {
		members, tasks = false, false
	}
	if g.panes["header"] != "" && g.dimensions["header"][1] != fixedHeaderHeight {
		return false
	}
	return (g.panes["members"] != "") == members && (g.panes["tasks"] != "") == tasks && (g.panes["header"] != "") == (members || tasks)
}

func appendTmCommand(args []string, command ...string) []string {
	if len(args) > 0 {
		args = append(args, tmux.Separator)
	}
	return append(args, command...)
}

func savePanelDimensions(s *State, target string, dimensions map[string][2]int) error {
	var args []string
	for _, role := range []string{"header", "members", "tasks"} {
		d, ok := dimensions[role]
		if !ok {
			continue
		}
		n := d[0]
		if role == "header" {
			n = fixedHeaderHeight
		}
		args = appendTmCommand(args, "set-option", "-w", "-t", target, "@csquad_size_"+role, strconv.Itoa(n))
	}
	if len(args) == 0 {
		return nil
	}
	_, err := tm(s, args...)
	return err
}
