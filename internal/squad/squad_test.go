package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, e := openStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.DB.Close() })
	e = st.update(func(s *State) error {
		*s = State{Version: 1, ID: "test", Root: t.TempDir(), Active: true, Members: map[string]*Member{}, Tasks: map[string]*Task{}, Questions: map[string]*Question{}}
		for _, id := range []string{"master", "a", "b"} {
			s.Members[id] = &Member{ID: id, Engine: config.Claude, State: MemberStateIdle, Generation: 1}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return st
}
func TestConcurrentClaimAcrossConnections(t *testing.T) {
	st := testStore(t)
	st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseReady, Dispatch: DispatchModeOpen}
		return nil
	})
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			other, e := openStore(st.Dir)
			if e != nil {
				t.Error(e)
				return
			}
			defer other.DB.Close()
			actor := "a"
			if i%2 == 1 {
				actor = "b"
			}
			if taskCommand(other, actor, []string{"claim", "T1"}, options{}) == nil {
				success.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("successful claims=%d, want 1", success.Load())
	}
}
func TestGateAndIndependentReview(t *testing.T) {
	st := testStore(t)
	st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseInProgress, Owner: "a", Participants: []string{"a", "b"}, Milestones: []Milestone{{"plan", true, MilestoneStatePending}}}
		return nil
	})
	if e := taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "done"}); e == nil {
		t.Fatal("unapproved gate accepted")
	}
	if e := taskCommand(st, "a", []string{"milestone", "T1"}, options{"name": "plan"}); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "a", []string{"gate", "T1"}, options{"name": "plan"}); e == nil {
		t.Fatal("worker approved gate")
	}
	if e := taskCommand(st, "master", []string{"gate", "T1"}, options{"name": "plan"}); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "a", []string{"submit", "T1"}, options{"summary": "done"}); e != nil {
		t.Fatal(e)
	}
	if e := taskCommand(st, "a", []string{"evidence", "T1"}, options{"kind": "review", "passed": "true", "summary": "fine"}); e == nil {
		t.Fatal("self review accepted")
	}
}
func TestMessageReplyRouting(t *testing.T) {
	st := testStore(t)
	var id string
	st.update(func(s *State) error { id = s.message("master", "a", "T1", "question", "").ID; return nil })
	if e := messageCommand(st, "b", []string{"reply", id}, options{"text": "wrong recipient"}); e == nil {
		t.Fatal("other member replied")
	}
	if e := messageCommand(st, "a", []string{"reply", id}, options{"text": "answer"}); e != nil {
		t.Fatal(e)
	}
	s, _ := st.read()
	m := s.Messages[len(s.Messages)-1]
	if m.To != "master" || m.ReplyTo != id || m.Task != "T1" {
		t.Fatalf("bad route %+v", m)
	}
}
func TestMergeRejectsChangedTarget(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	root := s.Root
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, e := git(root, args...); e != nil {
			t.Fatal(e)
		}
	}
	if e := taskCommand(st, "master", []string{"create", "change"}, options{"code": "true", "acceptance": "tests pass"}); e != nil {
		t.Fatal(e)
	}
	s, _ = st.read()
	var task *Task
	for _, v := range s.Tasks {
		task = v
	}
	wt := task.Workspace
	os.WriteFile(filepath.Join(wt, "hello.txt"), []byte("hello\n"), 0600)
	git(wt, "add", "hello.txt")
	if _, e := git(wt, "commit", "-m", "change"); e != nil {
		t.Fatal(e)
	}
	st.update(func(s *State) error {
		t := s.Tasks[task.ID]
		t.Owner = "a"
		t.State = TaskPhaseInProgress
		t.Participants = []string{"a", "b"}
		return nil
	})
	if e := taskCommand(st, "a", []string{"submit", task.ID}, options{"summary": "done"}); e != nil {
		t.Fatal(e)
	}
	sha, _ := git(wt, "rev-parse", "HEAD")
	for _, kind := range []string{"review", "test"} {
		if e := taskCommand(st, "b", []string{"evidence", task.ID}, options{"kind": kind, "sha": sha, "passed": "true", "summary": "verified"}); e != nil {
			t.Fatal(e)
		}
	}
	if e := taskCommand(st, "master", []string{"approve", task.ID}, options{}); e != nil {
		t.Fatal(e)
	}
	git(root, "commit", "--allow-empty", "-m", "target moved")
	if e := taskCommand(st, "master", []string{"merge", task.ID}, options{}); e == nil {
		t.Fatal("stale approval merged")
	}
}
func TestTransactionRollback(t *testing.T) {
	st := testStore(t)
	_ = st.update(func(s *State) error { s.ID = "corrupt"; return fmt.Errorf("abort") })
	s, _ := st.read()
	if s.ID != "test" {
		t.Fatal("rollback failed")
	}
}
