package codextest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

// MaybeFakeCodex runs the fake codex and exits when the binary was started
// as `codex`; it returns otherwise. Each test package's TestMain calls it.
func MaybeFakeCodex() {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != "codex" {
		return
	}
	os.Exit(fakeCodex(os.Args[1:]))
}

// fakeCodex: `codex app-server daemon start` links the control socket of
// $CODEX_HOME to $FAKE_CODEX_SOCKET (unless FAKE_CODEX_START_FAILS) and
// prints the daemon's JSON. Every argv goes to $FAKE_CODEX_LOG.
func fakeCodex(args []string) int {
	if log := os.Getenv("FAKE_CODEX_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
	}
	if strings.Join(args, " ") != "app-server daemon start" {
		fmt.Fprintf(os.Stderr, "fake codex: unexpected %q\n", args)
		return 2
	}
	if os.Getenv("FAKE_CODEX_START_FAILS") != "" {
		fmt.Fprintln(os.Stderr, "Error: cannot start the daemon")
		return 1
	}
	link := filepath.Join(os.Getenv("CODEX_HOME"), "app-server-control", "app-server-control.sock")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := LinkSocket(link, os.Getenv("FAKE_CODEX_SOCKET")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	b, _ := json.Marshal(map[string]any{"status": "started", "socketPath": link})
	fmt.Println(string(b))
	return 0
}

// LinkFake puts a `codex` link to the test binary into bin.
func LinkFake(t testing.TB, bin string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := testutil.LinkExe(exe, filepath.Join(bin, name)); err != nil {
		t.Fatal(err)
	}
}
