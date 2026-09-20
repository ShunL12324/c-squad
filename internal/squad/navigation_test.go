package squad

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func TestNavigationIsScopedAndRefreshesRoster(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	dir, e := os.MkdirTemp("", "csq-nav-")
	must(t, e)
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	_, e = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "unrelated", "sleep", "60")
	must(t, e)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Members["a"].Color = tmux.Mint
		s.Members["b"].Color = tmux.Mint
		s.Socket = socket
		s.Executable = "/tmp/csquad"
		for id, m := range s.Members {
			m.Session = "team-" + id
		}
		return nil
	}))
	s, _ := st.read()
	for _, m := range s.Members {
		_, e = tm(s, "new-session", "-d", "-s", m.Session, "sleep", "60")
		must(t, e)
	}
	unrelatedBefore, _ := tm(s, "show-options", "-Av", "-t", "=unrelated", "key-table")
	mouseBefore, _ := tm(s, "show-options", "-Av", "-t", "=unrelated", "mouse")
	before, _ := tm(s, "list-keys", "-T", "root")
	prefixBefore, _ := tm(s, "list-keys", "-T", "prefix")
	must(t, st.configureNavigation())
	first, e := st.read()
	must(t, e)
	rootTable, _ := navigationTables(st)
	_, e = tm(s, "bind-key", "-T", rootTable, "M-Right", "run-shell", "echo obsolete-navigation")
	must(t, e)
	must(t, st.configureNavigation())
	second, e := st.read()
	must(t, e)
	if first.Members["master"].Color == "" || first.Members["master"].Color != second.Members["master"].Color {
		t.Fatal("random color was not persisted")
	}
	if second.Members["a"].Color != tmux.Mint || second.Members["b"].Color != tmux.Mint {
		t.Fatal("explicit shared colors changed")
	}
	after, _ := tm(s, "list-keys", "-T", "root")
	prefixAfter, _ := tm(s, "list-keys", "-T", "prefix")
	if before != after || prefixBefore != prefixAfter {
		t.Fatal("modified global bindings")
	}
	table, e := tm(s, "show-options", "-Av", "-t", "=unrelated", "key-table")
	must(t, e)
	if table != unrelatedBefore {
		t.Fatal("modified unrelated session")
	}
	mouseAfter, e := tm(s, "show-options", "-Av", "-t", "=unrelated", "mouse")
	must(t, e)
	if mouseBefore != mouseAfter {
		t.Fatal("modified unrelated session mouse setting")
	}
	mouse, e := tm(s, "show-options", "-Av", "-t", "=team-master", "mouse")
	must(t, e)
	if mouse != "on" {
		t.Fatal("team mouse support disabled")
	}
	root, prefix := navigationTables(st)
	keys, e := tm(s, "list-keys", "-T", root)
	must(t, e)
	if strings.Contains(keys, "obsolete-navigation") || !strings.Contains(keys, prefix) {
		t.Fatal("missing navigation bindings")
	}
	if !strings.Contains(keys, "--direction next") || !strings.Contains(keys, "--direction previous") {
		t.Fatal("member switch keys are not bound:", keys)
	}
	must(t, st.update(func(s *State) error { s.Members["a"].State = MemberStateRemoved; return nil }))
	must(t, st.configureNavigation())
	labels, e := tm(s, "show-options", "-v", "-t", "=team-master", "status-format[1]")
	must(t, e)
	if strings.Contains(labels, "master") || strings.Contains(labels, "1:b") {
		t.Fatal("status bar still contains the member roster:", labels)
	}
	for _, action := range []string{"tasks", "detach"} {
		if !strings.Contains(labels, "range=user|"+action+",") {
			t.Fatalf("missing status button: %s", action)
		}
	}
	st.clearNavigation(s)
	if _, e = tm(s, "list-keys", "-T", root); e == nil {
		t.Fatal("key table leaked")
	}
}

func TestMemberSwitchKeysAreBoundAndConfigurable(t *testing.T) {
	st, socket := reproTeam(t, "180", "40")
	s, err := st.read()
	must(t, err)
	root, _ := navigationTables(st)
	keys, err := tm(s, "list-keys", "-T", root)
	must(t, err)
	for _, want := range []string{"M-Up", "M-Down", "--direction previous", "--direction next"} {
		if !strings.Contains(keys, want) {
			t.Fatalf("default member switch keys missing %q: %s", want, keys)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "python3", "testdata/member_switch.py", socket, "layout-master", "layout-a", "layout-b").CombinedOutput()
	if err != nil {
		t.Fatalf("member switching: %v\n%s", err, out)
	}
	t.Logf("%s", out)

	// A remapped pair replaces the defaults; an empty pair leaves the keys to
	// the agent CLI in the engine pane.
	for _, tt := range []struct{ previous, next, absent string }{{"M-p", "M-n", "M-Up"}, {"", "", "--direction"}} {
		must(t, st.update(func(s *State) error {
			cfg := config.Defaults()
			cfg.PreviousKey, cfg.NextKey = tt.previous, tt.next
			s.Config = &cfg
			return nil
		}))
		must(t, st.configureNavigation())
		keys, err = tm(s, "list-keys", "-T", root)
		must(t, err)
		if strings.Contains(keys, tt.absent) {
			t.Fatalf("keys %q/%q left %q bound: %s", tt.previous, tt.next, tt.absent, keys)
		}
		if tt.previous != "" && !strings.Contains(keys, tt.previous) {
			t.Fatalf("remapped key %q was not bound: %s", tt.previous, keys)
		}
	}
}

func TestMemberCycleWrapsAcrossManyMembers(t *testing.T) {
	st, socket := reproTeam(t, "180", "40")
	must(t, st.update(func(s *State) error {
		for _, id := range []string{"c", "d", "e", "f"} {
			s.Members[id] = &Member{ID: id, Engine: config.Claude, State: MemberStateIdle, Generation: 1, Session: "layout-" + id}
		}
		return nil
	}))
	s, err := st.read()
	must(t, err)
	for _, id := range []string{"c", "d", "e", "f"} {
		pane, e := tm(s, "new-session", "-d", "-s", "layout-"+id, "-x", "180", "-y", "40", "-P", "-F", "#{pane_id}", "cat")
		must(t, e)
		must(t, st.update(func(s *State) error { s.Members[id].Pane = pane; return nil }))
	}
	must(t, st.configureNavigation())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	hold := exec.CommandContext(ctx, "python3", "testdata/panel_layout.py", "hold", socket, "layout-master", "30")
	stdout, err := hold.StdoutPipe()
	must(t, err)
	must(t, hold.Start())
	defer func() { _ = hold.Process.Kill(); _, _ = hold.Process.Wait() }()
	if _, err = io.ReadFull(stdout, make([]byte, len("attached\n"))); err != nil {
		t.Fatalf("client never attached: %v", err)
	}
	client, err := tm(s, "list-clients", "-F", "#{client_name}")
	must(t, err)

	// Master sorts first and the ring is cyclic, matching the C-b 0-9 order.
	want := []string{"layout-a", "layout-b", "layout-c", "layout-d", "layout-e", "layout-f", "layout-master"}
	for _, session := range want {
		must(t, st.navigate(client, "next", ""))
		if got := clientSession(s, client); got != session {
			t.Fatalf("next stopped at %s, want %s", got, session)
		}
	}
	must(t, st.navigate(client, "previous", ""))
	if got := clientSession(s, client); got != "layout-f" {
		t.Fatalf("previous wrapped to %s, want layout-f", got)
	}
}
