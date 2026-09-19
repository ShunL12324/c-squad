package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func TestResponsivePanelVisibility(t *testing.T) {
	for _, tt := range []struct {
		view           panelView
		width          int
		members, tasks bool
	}{{panelBoth, 180, true, true}, {panelBoth, 110, false, true}, {panelBoth, 80, false, false}, {panelMembers, 110, true, false}, {panelHidden, 180, false, false}} {
		m, b := panelVisibility(tt.view, tt.width)
		if m != tt.members || b != tt.tasks {
			t.Fatalf("%s at %d: %v %v", tt.view, tt.width, m, b)
		}
	}
}
func TestPanelsPreserveEngineAndMasterLifecycle(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "csq-panels-")
	must(t, err)
	defer os.RemoveAll(dir)
	binary := filepath.Join(t.TempDir(), "csquad")
	build := exec.Command("go", "build", "-o", binary, "./cmd/csquad")
	build.Dir = "../.."
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %s: %v", out, err)
	}
	socket := filepath.Join(dir, "s")
	pane, err := process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "panel-master", "-x", "180", "-y", "35", "-P", "-F", "#{pane_id}", "cat")
	must(t, err)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Socket = socket
		s.Executable = binary
		s.PanelView = panelBoth
		delete(s.Members, "a")
		delete(s.Members, "b")
		s.Members["master"].Session = "panel-master"
		s.Members["master"].Pane = pane
		return nil
	}))
	s, _ := st.read()
	must(t, installMasterHook(st))
	must(t, st.configureNavigation())
	panes, err := tm(s, "list-panes", "-t", pane, "-F", "#{pane_id} #{@csquad_panel}")
	must(t, err)
	if len(strings.Split(panes, "\n")) != 3 {
		t.Fatalf("expected three panes: %s", panes)
	}
	sidebar := ""
	for _, line := range strings.Split(panes, "\n") {
		if strings.HasSuffix(line, " members") {
			sidebar = strings.Fields(line)[0]
		}
	}
	if sidebar == "" {
		t.Fatal("missing sidebar")
	}
	_, err = tm(s, "select-pane", "-t", sidebar)
	must(t, err)
	must(t, memberCommand(st, "master", []string{"interrupt", "master"}, options{}))
	_, err = tm(s, "send-keys", "-t", agentPane(s.Members["master"]), "-l", "engine-marker")
	must(t, err)
	captured, err := tm(s, "capture-pane", "-p", "-t", pane)
	must(t, err)
	if !strings.Contains(captured, "engine-marker") {
		t.Fatal("engine target followed panel focus")
	}

	// Resumed Codex sessions have an existing native ID and still become idle
	// when their engine pane is ready, even while a sidebar has focus.
	must(t, st.update(func(s *State) error {
		m := s.Members["master"]
		m.Engine = config.Codex
		m.EngineID = "resumed-thread"
		m.State = MemberStateStarting
		return nil
	}))
	_, err = tm(s, "send-keys", "-t", pane, "-l", "\nOpenAI Codex (test)\n› Ask Codex to do anything\n")
	must(t, err)
	must(t, st.refresh())
	ready, err := st.read()
	must(t, err)
	if ready.Members["master"].State != MemberStateIdle {
		t.Fatal("resumed Codex remained starting")
	}
	_, err = tm(s, "kill-pane", "-t", sidebar)
	must(t, err)
	time.Sleep(100 * time.Millisecond)
	current, err := st.read()
	must(t, err)
	if !current.Active {
		t.Fatal("closing a sidebar stopped the team")
	}
	must(t, st.setPanelView("hide", false))
	panes, err = tm(s, "list-panes", "-t", pane, "-F", "#{pane_id}")
	must(t, err)
	if panes != pane {
		t.Fatalf("panels not removed: %s", panes)
	}
	_, err = tm(s, "resize-window", "-t", pane, "-x", "80")
	must(t, err)
	must(t, st.setPanelView("both", false))
	panes, err = tm(s, "list-panes", "-t", pane, "-F", "#{pane_id}")
	must(t, err)
	if panes != pane {
		t.Fatal("narrow screen squeezed the engine")
	}
}
