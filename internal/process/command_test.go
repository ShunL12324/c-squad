package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagedCommandCancellationStopsChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child")
	cmd := Command(ctx, "", nil, "sh", "-c", "sleep 60 & echo $! > \"$1\"; wait", "sh", pidFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Always reap the group even if readiness fails.
	defer func() { cancel(); _ = cmd.Wait() }()
	var child Identity
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err == nil {
				all, err := Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				child = all[pid]
				if child.PID != 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child.PID == 0 {
		t.Fatal("child never started")
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancellation should fail the command")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		all, err := Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !Alive(child, all) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("command cancellation left its child alive")
}

func TestRunStdoutEnvSeparatesStderr(t *testing.T) {
	out, err := RunStdoutEnv("", nil, "sh", "-c", `printf 'warning\n' >&2; printf '  [{"sessionId":"s-1"}]  \n'`)
	if err != nil || out != `[{"sessionId":"s-1"}]` {
		t.Fatalf("structured stdout was polluted by stderr: out=%q err=%v", out, err)
	}
	out, err = RunStdoutEnv("", nil, "sh", "-c", `printf 'partial'; printf 'helper failed\n' >&2; exit 7`)
	if out != "partial" || err == nil || !strings.Contains(err.Error(), "helper failed") {
		t.Fatalf("failure lost stdout or stderr diagnosis: out=%q err=%v", out, err)
	}
}
