package process

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestStopTreeThawsOnEnumerationFailure(t *testing.T) {
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	before, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	identity := before[child.Process.Pid]
	injected := errors.New("enumeration unavailable")
	calls := 0
	err = stopTree(identity.PID, identity.Start, func() (map[int]Identity, error) {
		calls++
		if calls == 2 {
			return nil, injected
		}
		return Snapshot()
	})
	if !errors.Is(err, injected) {
		t.Fatalf("lost enumeration failure: %v", err)
	}
	after, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !Alive(identity, after) || strings.Contains(after[identity.PID].Stat, "T") {
		t.Fatal("failed cleanup should leave the original child alive and resumed")
	}
}
