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

// Issue #27: an argument ending in ";" must reach tmux as a literal value, not
// split the command, while a bare Separator still separates commands.
func TestTrailingSemicolonStaysLiteral(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "csq-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	client := Client{Socket: filepath.Join(dir, "s")}
	if _, err = process.Run("", "tmux", "-f", "/dev/null", "-S", client.Socket, "new-session", "-d", "-s", "x", "sleep", "60"); err != nil {
		t.Fatal(err)
	}
	defer client.Run("kill-server")
	for _, value := range []string{";", `\;`, ";;", "a;", `a\;`, `a\\;`, "mid;dle", "K=v;", "x ; y"} {
		if _, err = client.Run("set-option", "-t", "=x", "@value", value, Separator, "set-option", "-t", "=x", "@after", "set"); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		got, err := client.Run("show-options", "-v", "-t", "=x", "@value")
		if err != nil || got != value {
			t.Fatalf("value %q arrived as %q (%v)", value, got, err)
		}
		after, err := client.Run("show-options", "-v", "-t", "=x", "@after")
		if err != nil || after != "set" {
			t.Fatalf("separator after %q lost: %q (%v)", value, after, err)
		}
		if _, err = client.Run("set-option", "-u", "-t", "=x", "@after"); err != nil {
			t.Fatal(err)
		}
	}
}

// A real composite command: two commands joined by Separator both run, and a
// literal ";" argument inside the first stays a value.
func TestSeparatorJoinsCommandsWithLiteralSemicolonValues(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "csq-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	client := Client{Socket: filepath.Join(dir, "s")}
	if _, err = process.Run("", "tmux", "-f", "/dev/null", "-S", client.Socket, "new-session", "-d", "-s", "x", "sleep", "60"); err != nil {
		t.Fatal(err)
	}
	defer client.Run("kill-server")
	out, err := client.Run("set-option", "-t", "=x", "@one", ";", Separator, "set-option", "-t", "=x", "@two", `\;`, Separator, "display-message", "-p", "-t", "=x", "#{@one}|#{@two}")
	if err != nil || out != `;|\;` {
		t.Fatalf("composite command = %q, %v; want both options set literally", out, err)
	}
}
