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
