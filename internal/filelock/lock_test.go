package filelock

import "testing"

func TestContentionAndIdempotentRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir, "member-alice", false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if unexpected, err := Acquire(dir, "member-alice", true); err == nil {
		unexpected()
		t.Fatal("a competing descriptor acquired the same lock")
	}
	other, err := Acquire(dir, "member-bob", true)
	if err != nil {
		t.Fatalf("unrelated member was blocked: %v", err)
	}
	other()
	release()
	release()
	next, err := Acquire(dir, "member-alice", true)
	if err != nil {
		t.Fatalf("released lock remained held: %v", err)
	}
	next()
}
