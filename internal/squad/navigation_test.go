package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if !strings.Contains(keys, "M-Right") || !strings.Contains(keys, prefix) || !strings.Contains(keys, "MouseDown1Status") {
		t.Fatal("missing navigation bindings")
	}
	must(t, st.update(func(s *State) error { s.Members["a"].State = MemberStateRemoved; return nil }))
	must(t, st.configureNavigation())
	labels, e := tm(s, "show-options", "-v", "-t", "=team-master", "status-format[0]")
	must(t, e)
	if strings.Contains(labels, ":a ") || !strings.Contains(labels, "0:master") || !strings.Contains(labels, "1:b") {
		t.Fatal(labels)
	}
	if !strings.Contains(labels, "#[range=user|1,") {
		t.Fatal("member click range did not follow roster change:", labels)
	}
	st.clearNavigation(s)
	if _, e = tm(s, "list-keys", "-T", root); e == nil {
		t.Fatal("key table leaked")
	}
}
