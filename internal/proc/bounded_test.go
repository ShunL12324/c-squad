package proc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// sleeper is a process that runs until killed: the test binary in
// TestMain's helper mode.
func sleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "PROC_TEST_SLEEP=1")
	KillGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestMain(m *testing.M) {
	if os.Getenv("PROC_TEST_SLEEP") != "" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSweep(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	young, old := sleeper(t), sleeper(t)
	forgetYoung := Track(dir, young, "statusline", now)
	Track(dir, old, "command", now.Add(-2*time.Minute))
	// A record whose pid now runs another process: never killed.
	other := sleeper(t)
	b, _ := json.Marshal(Tracked{PID: other.Process.Pid, Ident: "someone else", At: now.Add(-time.Hour).UnixMilli()})
	os.WriteFile(filepath.Join(dir, strconv.Itoa(other.Process.Pid)+".json"), b, 0o600)

	running := Sweep(dir, time.Minute, now)
	if len(running) != 1 || running[0].PID != young.Process.Pid || running[0].By != "statusline" {
		t.Errorf("running: %+v", running)
	}
	done := make(chan error, 1)
	go func() { done <- old.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the old process was not killed")
	}
	if !Running(other.Process.Pid) || !Running(young.Process.Pid) {
		t.Error("a process that was not due was killed")
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("records: %v", left)
	}
	forgetYoung()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("records: %v", left)
	}
	if runtime.GOOS != "windows" && Running(old.Process.Pid) {
		t.Error("the old process still runs")
	}
}
