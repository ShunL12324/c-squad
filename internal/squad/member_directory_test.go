package squad

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemberDirectoryValidationBeforeRestart(t *testing.T) {
	st := testStore(t)
	before, err := st.read()
	must(t, err)
	err = memberCommand(st, "master", []string{"restart", "a"}, options{"cwd": filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("accepted a missing directory")
	}
	after, err := st.read()
	must(t, err)
	if after.Members["a"].Generation != before.Members["a"].Generation || after.Members["a"].State != before.Members["a"].State {
		t.Fatal("invalid directory interrupted the member")
	}
}

func TestMemberDirectoryResolution(t *testing.T) {
	cwd, err := os.Getwd()
	must(t, err)
	got, err := memberDirectory("/unused", ".")
	must(t, err)
	if got != cwd {
		t.Fatalf("relative path resolved to %s, want %s", got, cwd)
	}
	got, err = memberDirectory("/task/worktree", "")
	must(t, err)
	if got != "/task/worktree" {
		t.Fatal("default workspace changed")
	}
	file := filepath.Join(t.TempDir(), "file")
	must(t, os.WriteFile(file, []byte("test"), 0600))
	if _, err = memberDirectory("", file); err == nil {
		t.Fatal("accepted a file as a directory")
	}
}
