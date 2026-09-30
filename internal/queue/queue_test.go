package queue

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestQueueLifecycle(t *testing.T) {
	s := open(t)
	a, err := s.Add(Task{Prompt: "first", Cwd: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Add(Task{Prompt: "second\nmore", Cwd: "/b", Agent: "reviewer"})
	if pos, _ := s.Position(b.ID); pos != 2 {
		t.Fatalf("position of second task = %d, want 2", pos)
	}
	if ok, err := s.Claim(a.ID); !ok || err != nil {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, _ := s.Claim(a.ID); ok {
		t.Fatal("claimed a task twice")
	}
	if pos, _ := s.Position(b.ID); pos != 1 {
		t.Fatalf("position after claim = %d, want 1", pos)
	}
	if err := s.SetSession(a.ID, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(a.ID); err == nil || !strings.Contains(err.Error(), "claude stop abc123") {
		t.Fatalf("cancel of launched task: %v", err)
	}
	if err := s.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Claim(b.ID); ok {
		t.Fatal("claimed a cancelled task")
	}
	got, _ := s.Get(b.ID)
	if got.State != Cancelled || got.Agent != "reviewer" || got.DisplayName() != "T2 · second" {
		t.Fatalf("task = %+v, name %q", got, got.DisplayName())
	}
	if _, err := s.Get(99); err == nil {
		t.Fatal("found a missing task")
	}
}

func TestParseID(t *testing.T) {
	for in, want := range map[string]int64{"T12": 12, "t3": 3, "7": 7} {
		if got, err := ParseID(in); err != nil || got != want {
			t.Fatalf("ParseID(%q) = %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "T", "x1", "T-1", "0"} {
		if _, err := ParseID(bad); err == nil {
			t.Fatalf("ParseID(%q) accepted", bad)
		}
	}
}

func TestSummary(t *testing.T) {
	if got := Summary("\n  hello world  \nnext", 5); got != "hell…" {
		t.Fatalf("Summary = %q", got)
	}
}

func TestFinishAndPrune(t *testing.T) {
	s := open(t)
	a, _ := s.Add(Task{Prompt: "a", Cwd: "/a"})
	b, _ := s.Add(Task{Prompt: "b", Cwd: "/a"})
	if ok, _ := s.Claim(a.ID); !ok {
		t.Fatal("claim")
	}
	if err := s.Finish(b.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(b.ID); got.State != Queued {
		t.Fatalf("finished a queued task: %s", got.State)
	}
	if err := s.Finish(a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(a.ID)
	if got.State != Finished || got.EndedAt.IsZero() || !got.State.Ended() {
		t.Fatalf("finished task = %+v", got)
	}
	if n, _ := s.Prune(got.EndedAt); n != 0 {
		t.Fatalf("pruned %d tasks that ended at the cutoff", n)
	}
	if n, _ := s.Prune(got.EndedAt.Add(time.Millisecond)); n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
	if _, err := s.Get(a.ID); err == nil {
		t.Fatal("pruned task still present")
	}
	if _, err := s.Get(b.ID); err != nil {
		t.Fatal("pruned a queued task")
	}
}
