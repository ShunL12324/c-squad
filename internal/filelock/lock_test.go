package filelock

import "testing"

func TestContentionAndIdempotentRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir, "dispatcher", false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if unexpected, err := Acquire(dir, "dispatcher", true); err == nil {
		unexpected()
		t.Fatal("a competing descriptor acquired the same lock")
	}
	other, err := Acquire(dir, "other", true)
	if err != nil {
		t.Fatalf("unrelated lock was blocked: %v", err)
	}
	other()
	release()
	release()
	next, err := Acquire(dir, "dispatcher", true)
	if err != nil {
		t.Fatalf("released lock remained held: %v", err)
	}
	next()
}
