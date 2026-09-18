package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/process"
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
	before, _ := tm(s, "list-keys", "-T", "root")
	prefixBefore, _ := tm(s, "list-keys", "-T", "prefix")
	must(t, st.configureNavigation())
	must(t, st.configureNavigation())
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
	root, prefix := navigationTables(st)
	keys, e := tm(s, "list-keys", "-T", root)
	must(t, e)
	if !strings.Contains(keys, "M-Right") || !strings.Contains(keys, prefix) {
		t.Fatal("missing navigation bindings")
	}
	must(t, st.update(func(s *State) error { s.Members["a"].State = MemberStateRemoved; return nil }))
	must(t, st.configureNavigation())
	labels, e := tm(s, "show-options", "-v", "-t", "=team-master", "status-format[0]")
	must(t, e)
	if strings.Contains(labels, ":a ") || !strings.Contains(labels, "0:master") || !strings.Contains(labels, "1:b") {
		t.Fatal(labels)
	}
	st.clearNavigation(s)
	if _, e = tm(s, "list-keys", "-T", root); e == nil {
		t.Fatal("key table leaked")
	}
}
