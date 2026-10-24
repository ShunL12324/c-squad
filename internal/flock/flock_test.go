package flock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A child process holds the lock until it is killed.
func TestMain(m *testing.M) {
	if p := os.Getenv("FLOCK_HOLD"); p != "" {
		if _, err := Try(p); err != nil {
			os.Exit(3)
		}
		os.Stdout.WriteString("held\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestLockAcrossProcesses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "logs", "T1.lock")
	if Held(p) {
		t.Fatal("a missing file is free")
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "FLOCK_HOLD="+p)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := out.Read(buf); err != nil || string(buf) != "held\n" {
		t.Fatalf("child: %q %v", buf, err)
	}
	if !Held(p) {
		t.Error("held by the child")
	}
	if _, err := Try(p); !errors.Is(err, ErrHeld) {
		t.Errorf("Try: %v", err)
	}
	start := time.Now()
	if _, err := Wait(p, 200*time.Millisecond); !errors.Is(err, ErrHeld) || time.Since(start) < 200*time.Millisecond {
		t.Errorf("Wait: %v", err)
	}
	// The holder dies without releasing: the OS frees the lock.
	cmd.Process.Kill()
	cmd.Wait()
	l, err := Wait(p, 2*time.Second)
	if err != nil {
		t.Fatalf("after the holder died: %v", err)
	}
	l.Release()
	l.Release()
	if Held(p) {
		t.Error("released")
	}
}
