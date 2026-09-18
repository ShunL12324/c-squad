package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/process"
)

func TestExactTargetPreservesArgumentsAndRejectsPrefix(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	// Unix sockets have a short path limit, including on macOS test directories.
	dir, err := os.MkdirTemp("", "csq-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	client := Client{Socket: filepath.Join(dir, "s")}
	if _, err = process.Run("", "tmux", "-f", "/dev/null", "-S", client.Socket, "new-session", "-d", "-s", "alice-extra", "sleep", "60"); err != nil {
		t.Fatal(err)
	}
	defer client.Run("kill-server")
	args := []string{"display-message", "-p", "-t", "=alice-extra:", "#{session_name}"}
	original := append([]string(nil), args...)
	got, err := client.Run(args...)
	if err != nil || got != "alice-extra" {
		t.Fatalf("exact lookup: %q, %v", got, err)
	}
	if !reflect.DeepEqual(args, original) {
		t.Fatal("client modified caller arguments")
	}
	if _, err = client.Run("display-message", "-p", "-t", "=alice:", "#{session_name}"); err == nil || !strings.Contains(err.Error(), "no exact tmux session") {
		t.Fatalf("missing exact target matched an unrelated session: %v", err)
	}
}
