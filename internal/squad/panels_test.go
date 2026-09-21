package squad

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/process"
)

func TestResponsivePanelVisibility(t *testing.T) {
	for _, tt := range []struct {
		view           panelView
		width          int
		members, tasks bool
	}{{"", 180, true, true}, {panelBoth, 180, true, true}, {panelBoth, 110, true, false}, {panelTasks, 110, true, false}, {panelBoth, 80, false, false}, {panelMembers, 110, true, false}, {panelHidden, 180, false, false}} {
		m, b := panelVisibility(tt.view, tt.width)
		if m != tt.members || b != tt.tasks {
			t.Fatalf("%s at %d: %v %v", tt.view, tt.width, m, b)
		}
	}
}
func TestPanelsPreserveEngineAndMasterLifecycle(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
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
		s.Tasks["T1"] = &Task{ID: "T1", Title: "Verify task board", Owner: "a", State: TaskPhaseInProgress, Description: "Read the entire task detail", Milestones: []Milestone{{Name: "Investigate", State: MilestoneStateReported}, {Name: "Review", State: MilestoneStateAwaitingApproval, Gate: true}}}
		s.Members["a"].Session = "panel-worker"
		delete(s.Members, "b")
		s.Members["master"].Session = "panel-master"
		s.Members["master"].Pane = pane
		return nil
	}))
	s, _ := st.read()
	workerPane, err := tm(s, "new-session", "-d", "-s", "panel-worker", "-x", "180", "-y", "35", "-P", "-F", "#{pane_id}", "cat")
	must(t, err)
	must(t, st.update(func(s *State) error { s.Members["a"].Pane = workerPane; return nil }))
	_, err = tm(s, "set-option", "-p", "-t", pane, "window-style", "bg=colour53")
	must(t, err)
	must(t, installMasterHook(st))
	must(t, st.configureNavigation())

	panes, err := tm(s, "list-panes", "-t", pane, "-F", "#{pane_id} #{@csquad_panel}")
	must(t, err)
	if len(strings.Split(panes, "\n")) != 4 {
		t.Fatalf("expected four panes: %s", panes)
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
	panelStyle, err := tm(s, "show-options", "-pv", "-t", sidebar, "window-style")
	must(t, err)
	engineStyle, err := tm(s, "show-options", "-pv", "-t", pane, "window-style")
	must(t, err)
	if panelStyle != "bg=colour234" || engineStyle != "bg=colour53" {
		t.Fatalf("panel background leaked or engine theme changed: %s / %s", panelStyle, engineStyle)
	}
	if python, lookupErr := exec.LookPath("python3"); lookupErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, clickErr := exec.CommandContext(ctx, python, "testdata/panel_mouse.py", socket, "panel-master", "panel-worker").CombinedOutput()
		if clickErr != nil {
			t.Fatalf("mouse navigation: %v\n%s", clickErr, output)
		}
	}
	colored, err := tm(s, "capture-pane", "-e", "-p", "-t", sidebar)
	must(t, err)
	nameColored := false
	for _, line := range strings.Split(colored, "\n") {
		if strings.Contains(line, "master") && strings.Contains(line, "\x1b[") {
			nameColored = true
		}
	}
	if !nameColored {
		t.Fatal("inherited NO_COLOR disabled member colors")
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
	for _, size := range []struct{ width, height string }{{"110", "25"}, {"220", "80"}, {"100", "22"}, {"180", "40"}} {
		_, err = tm(s, "resize-window", "-t", pane, "-x", size.width, "-y", size.height)
		must(t, err)
		must(t, st.setPanelView("both", false))
		geometry, err := tm(s, "list-panes", "-t", pane, "-F", "#{@csquad_panel}:#{pane_width}:#{pane_height}")
		must(t, err)
		if !strings.Contains(geometry, "members:28:") || !strings.Contains(geometry, "header:"+size.width+":3") {
			t.Fatalf("unstable chrome after resize to %v: %s", size, geometry)
		}
		if (size.width == "110" || size.width == "100") && strings.Contains(geometry, "tasks:") {
			t.Fatalf("compact layout kept tasks instead of members: %s", geometry)
		}
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

// reproTeam builds a team whose members are already running, so layout and
// navigation can be exercised without launching real engines.
func reproTeam(t *testing.T, newbieWidth, newbieHeight string) (*Store, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	dir, err := os.MkdirTemp("", "csq-layout-")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	binary := filepath.Join(t.TempDir(), "csquad")
	build := exec.Command("go", "build", "-o", binary, "./cmd/csquad")
	build.Dir = "../.."
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build: %s: %v", out, buildErr)
	}
	socket := filepath.Join(dir, "s")
	pane, err := process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "layout-master", "-x", "180", "-y", "40", "-P", "-F", "#{pane_id}", "cat")
	must(t, err)
	t.Cleanup(func() { process.Run("", "tmux", "-S", socket, "kill-server") })
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Socket = socket
		s.Executable = binary
		s.PanelView = panelBoth
		s.Members["master"].Session = "layout-master"
		s.Members["master"].Pane = pane
		s.Members["a"].Session = "layout-a"
		s.Members["b"].Session = "layout-b"
		return nil
	}))
	s, _ := st.read()
	// "a" was already visited at the client's size; "b" stands in for a freshly
	// created member, born detached at a geometry no client is using.
	for _, spec := range []struct{ id, width, height string }{{"a", "180", "40"}, {"b", newbieWidth, newbieHeight}} {
		member, e := tm(s, "new-session", "-d", "-s", "layout-"+spec.id, "-x", spec.width, "-y", spec.height, "-P", "-F", "#{pane_id}", "cat")
		must(t, e)
		must(t, st.update(func(s *State) error { s.Members[spec.id].Pane = member; return nil }))
	}
	must(t, st.configureNavigation())
	return st, socket
}

func TestNewMemberClickKeepsOuterLayoutStable(t *testing.T) {
	_, socket := reproTeam(t, "140", "42")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "stable", socket, "layout-master", "layout-b").CombinedOutput()
	if err != nil {
		t.Fatalf("layout stability: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

func TestNewSessionsUseTheAttachedClientGeometry(t *testing.T) {
	st, socket := reproTeam(t, "140", "42")
	s, err := st.read()
	must(t, err)
	if width, height := teamWindowSize(s); width != "140" || height != "42" {
		t.Fatalf("unattached team should fall back to the fixed size, got %sx%s", width, height)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	hold := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "hold", socket, "layout-master", "20")
	stdout, err := hold.StdoutPipe()
	must(t, err)
	must(t, hold.Start())
	defer func() { _ = hold.Process.Kill(); _, _ = hold.Process.Wait() }()
	buf := make([]byte, len("attached\n"))
	if _, err = io.ReadFull(stdout, buf); err != nil {
		t.Fatalf("client never attached: %v", err)
	}
	width, height := teamWindowSize(s)
	if width != "180" || height != "38" {
		t.Fatalf("new sessions ignored the attached client: %sx%s", width, height)
	}
}

// Hold the layout lock to deterministically put the terminal resize between
// the initial compact-mode decision and completion of the Tasks toggle.
func TestTasksToggleDoesNotOpenPopupAfterConcurrentResize(t *testing.T) {
	st, socket := reproTeam(t, "180", "40")
	must(t, st.setPanelView("members", false))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hold := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "controlled", socket, "layout-master")
	stdout, err := hold.StdoutPipe()
	must(t, err)
	stdin, err := hold.StdinPipe()
	must(t, err)
	must(t, hold.Start())
	defer func() { _ = hold.Process.Kill(); _, _ = hold.Process.Wait() }()
	reader := bufio.NewReader(stdout)
	_, err = reader.ReadString('\n')
	must(t, err)
	s, err := st.read()
	must(t, err)
	client, err := tm(s, "list-clients", "-F", "#{client_name}")
	must(t, err)
	unlock, err := filelock.Acquire(st.Dir, "panels", false)
	must(t, err)
	defer unlock()
	done := make(chan error, 1)
	go func() { done <- st.openUI(options{"view": "tasks", "client": client}, true) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := st.read()
		must(t, err)
		if current.PanelView == panelBoth {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("toggle did not reach the layout lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err = io.WriteString(stdin, "100\n")
	must(t, err)
	_, err = reader.ReadString('\n')
	must(t, err)
	unlock()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(3 * time.Second):
		_, _ = tm(s, "display-popup", "-C", "-c", client)
		t.Fatal("wide-screen Tasks toggle opened a popup after the resize")
	}
	// A new, explicit click on the now-narrow client must still open a popup.
	type popupResult struct {
		opened bool
		err    error
	}
	popup := make(chan popupResult, 1)
	go func() { opened, err := st.compactPanelPopup(s, "tasks", client); popup <- popupResult{opened, err} }()
	select {
	case result := <-popup:
		t.Fatalf("narrow popup returned before dismissal: %+v", result)
	case <-time.After(500 * time.Millisecond):
	}
	_, err = tm(s, "display-popup", "-C", "-c", client)
	must(t, err)
	select {
	case result := <-popup:
		// display-popup -C terminates the child, so tmux may return its signal status.
		if !result.opened {
			t.Fatal("explicit narrow popup was not opened")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("popup did not close")
	}
}

func TestNativeBorderDragAndSwitchLatency(t *testing.T) {
	st, socket := reproTeam(t, "280", "77")
	s, err := st.read()
	must(t, err)
	pane, err := tm(s, "new-session", "-d", "-s", "layout-c", "-x", "280", "-y", "77", "-P", "-F", "#{pane_id}", "cat")
	must(t, err)
	must(t, st.update(func(s *State) error {
		s.Members["c"] = &Member{ID: "c", Engine: config.Codex, State: MemberStateIdle, Generation: 1, Session: "layout-c", Pane: pane}
		return nil
	}))
	must(t, st.configureNavigation())
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "native", socket, "layout-master").CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("native drag/latency: %v", err)
	}
}
